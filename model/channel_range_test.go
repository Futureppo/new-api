package model

import (
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func seedChannelRange(t *testing.T, ids []int) {
	t.Helper()
	for i, id := range ids {
		require.NoError(t, DB.Create(&Channel{Id: id, Name: "range-test", Key: "test", Status: i%3 + 1}).Error)
		for _, name := range []string{"model-a", "model-b"} {
			require.NoError(t, DB.Create(&Ability{ChannelId: id, Group: "default", Model: name, Enabled: true}).Error)
		}
	}
}

func TestDeleteChannelsByIDRange(t *testing.T) {
	for _, tc := range []struct {
		name       string
		start, end int
		deleted    []int
		remaining  []int
	}{
		{"inclusive_sparse_all_statuses", 100, 200, []int{100, 150, 200}, []int{99, 201}},
		{"single", 150, 150, []int{150}, []int{99, 100, 200, 201}},
		{"empty", 101, 149, nil, []int{99, 100, 150, 200, 201}},
		{"large_sparse_range", 1, 2000000000, []int{99, 100, 150, 200, 201}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			seedChannelRange(t, []int{99, 100, 150, 200, 201})
			count, ids, err := DeleteChannelsByIDRange(tc.start, tc.end)
			require.NoError(t, err)
			require.Equal(t, int64(len(tc.deleted)), count)
			require.ElementsMatch(t, tc.deleted, ids)
			var remaining []int
			require.NoError(t, DB.Model(&Channel{}).Order("id").Pluck("id", &remaining).Error)
			require.ElementsMatch(t, tc.remaining, remaining)
			var abilities []Ability
			require.NoError(t, DB.Find(&abilities).Error)
			require.Len(t, abilities, 2*len(tc.remaining))
			for _, ability := range abilities {
				require.Contains(t, tc.remaining, ability.ChannelId)
			}
		})
	}
}

func TestDeleteChannelsByIDRangeChunksAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "multiple_chunks"
		if fail {
			name = "rollback_second_chunk"
		}
		t.Run(name, func(t *testing.T) {
			truncateTables(t)
			ids := make([]int, 205)
			for i := range ids {
				ids[i] = i + 1
			}
			seedChannelRange(t, ids)
			if fail {
				calls := 0
				require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register("test:range_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "abilities" {
						calls++
						if calls == 2 {
							tx.AddError(errors.New("injected ability deletion failure"))
						}
					}
				}))
				t.Cleanup(func() { require.NoError(t, DB.Callback().Delete().Remove("test:range_failure")) })
			}
			count, deletedIDs, err := DeleteChannelsByIDRange(1, 205)
			var channelsLeft, abilitiesLeft int64
			require.NoError(t, DB.Model(&Channel{}).Count(&channelsLeft).Error)
			require.NoError(t, DB.Model(&Ability{}).Count(&abilitiesLeft).Error)
			if fail {
				require.ErrorContains(t, err, "injected ability deletion failure")
				require.Zero(t, count)
				require.Empty(t, deletedIDs)
				require.EqualValues(t, 205, channelsLeft)
				require.EqualValues(t, 410, abilitiesLeft)
			} else {
				require.NoError(t, err)
				require.EqualValues(t, 205, count)
				require.Equal(t, ids, deletedIDs)
				require.Zero(t, channelsLeft)
				require.Zero(t, abilitiesLeft)
			}
		})
	}
}

func TestDeleteChannelsByIDRangeRejectsInvalidRange(t *testing.T) {
	truncateTables(t)
	seedChannelRange(t, []int{1})
	for _, ids := range [][2]int{{0, 1}, {-1, 1}, {1, 0}, {2, 1}} {
		count, deleted, err := DeleteChannelsByIDRange(ids[0], ids[1])
		require.Error(t, err)
		require.Zero(t, count)
		require.Empty(t, deleted)
	}
	var count int64
	DB.Model(&Channel{}).Count(&count)
	require.EqualValues(t, 1, count)
}

func TestDeleteChannelsByIDRangeServerDialects(t *testing.T) {
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	for name, dialect := range map[string]gorm.Dialector{
		"mysql":    mysql.New(mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}),
		"postgres": postgres.New(postgres.Config{Conn: sqlDB}),
	} {
		t.Run(name, func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true})
			require.NoError(t, err)
			var statements []string
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:range_query", func(tx *gorm.DB) {
				statements = append(statements, tx.Statement.SQL.String())
				*tx.Statement.Dest.(*[]int) = []int{100, 200}
			}))
			require.NoError(t, db.Callback().Delete().After("gorm:delete").Register("test:range_delete", func(tx *gorm.DB) {
				statements = append(statements, tx.Statement.SQL.String())
				tx.RowsAffected = 2
			}))
			original := DB
			DB = db
			t.Cleanup(func() { DB = original })
			count, ids, err := DeleteChannelsByIDRange(100, 200)
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
			require.Equal(t, []int{100, 200}, ids)
			require.Len(t, statements, 3)
			for i, table := range []string{"channels", "channels", "abilities"} {
				if name == "postgres" {
					require.Contains(t, statements[i], `"`+table+`"`)
					require.Contains(t, statements[i], "$1")
				} else {
					require.Contains(t, statements[i], "`"+table+"`")
					require.Contains(t, statements[i], "?")
				}
			}
		})
	}
}

func TestExistingBatchDeleteChannels(t *testing.T) {
	truncateTables(t)
	seedChannelRange(t, []int{1, 2, 3})
	require.NoError(t, BatchDeleteChannels([]int{1, 3}))
	var channel Channel
	require.NoError(t, DB.First(&channel).Error)
	require.Equal(t, 2, channel.Id)
	require.Equal(t, common.ChannelStatusManuallyDisabled, channel.Status)
	var count int64
	require.NoError(t, DB.Model(&Ability{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
}

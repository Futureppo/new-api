package model

import (
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestDeleteChannelsByExactName(t *testing.T) {
	names := []string{"Model", "Model", "Model", "model", "Model ", " Model", "Model-extra", "Módel", "模型", "模型", "x%_y", "xABCy", "' OR 1=1 --"}
	for _, tc := range []struct {
		name string
		ids  []int
	}{
		{"Model", []int{1, 2, 3}}, {"model", []int{4}}, {"Model ", []int{5}},
		{" Model", []int{6}}, {"模型", []int{9, 10}}, {"x%_y", []int{11}},
		{"' OR 1=1 --", []int{13}}, {"Mod", nil}, {"absent", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			truncateTables(t)
			for i, name := range names {
				require.NoError(t, DB.Create(&Channel{Id: i + 1, Name: name, Key: "test", Status: i%3 + 1}).Error)
				require.NoError(t, DB.Create(&Ability{ChannelId: i + 1, Group: "default", Model: "test"}).Error)
			}
			count, ids, err := DeleteChannelsByExactName(tc.name)
			require.NoError(t, err)
			require.EqualValues(t, len(tc.ids), count)
			require.ElementsMatch(t, tc.ids, ids)
			var channels []Channel
			require.NoError(t, DB.Find(&channels).Error)
			require.Len(t, channels, len(names)-len(tc.ids))
			for _, channel := range channels {
				require.NotContains(t, tc.ids, channel.Id)
			}
			var abilities []Ability
			require.NoError(t, DB.Find(&abilities).Error)
			require.Len(t, abilities, len(channels))
			for _, ability := range abilities {
				require.NotContains(t, tc.ids, ability.ChannelId)
			}
		})
	}
	for _, name := range []string{"", " ", "\t\n", "\u3000"} {
		count, ids, err := DeleteChannelsByExactName(name)
		require.Error(t, err)
		require.Zero(t, count)
		require.Empty(t, ids)
	}
}

func TestDeleteChannelsByExactNameCaseInsensitiveCollation(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	original := DB
	DB = db
	t.Cleanup(func() { DB = original; require.NoError(t, sqlDB.Close()) })
	// Deliberately use a case-insensitive database column to exercise the Go
	// equality guard rather than relying on SQLite's default binary collation.
	require.NoError(t, db.Exec("CREATE TABLE channels (id INTEGER PRIMARY KEY, name TEXT COLLATE NOCASE)").Error)
	require.NoError(t, db.AutoMigrate(&Ability{}))
	for i, name := range []string{"Model", "model", "MODEL"} {
		require.NoError(t, db.Table("channels").Create(map[string]any{"id": i + 1, "name": name}).Error)
	}
	count, ids, err := DeleteChannelsByExactName("Model")
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.Equal(t, []int{1}, ids)
	var remaining int64
	require.NoError(t, db.Model(&Channel{}).Count(&remaining).Error)
	require.EqualValues(t, 2, remaining)
}

func TestDeleteChannelsByExactNameChunksAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "rollback"
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
				require.NoError(t, DB.Callback().Delete().Before("gorm:delete").Register("test:name_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "abilities" {
						calls++
						if calls == 2 {
							tx.AddError(errors.New("injected name deletion failure"))
						}
					}
				}))
				t.Cleanup(func() { require.NoError(t, DB.Callback().Delete().Remove("test:name_failure")) })
			}
			count, deletedIDs, err := DeleteChannelsByExactName("range-test")
			var channelsLeft, abilitiesLeft int64
			require.NoError(t, DB.Model(&Channel{}).Count(&channelsLeft).Error)
			require.NoError(t, DB.Model(&Ability{}).Count(&abilitiesLeft).Error)
			if fail {
				require.Error(t, err)
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

func TestDeleteChannelsByExactNameServerDialects(t *testing.T) {
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
			require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:name_query", func(tx *gorm.DB) {
				statements = append(statements, tx.Statement.SQL.String())
				require.Equal(t, []any{"Model"}, tx.Statement.Vars)
				*tx.Statement.Dest.(*[]Channel) = []Channel{{Id: 1, Name: "Model"}, {Id: 2, Name: "model"}, {Id: 3, Name: "Model "}, {Id: 4, Name: "Model"}, {Id: 5, Name: "Módel"}}
			}))
			require.NoError(t, db.Callback().Delete().After("gorm:delete").Register("test:name_delete", func(tx *gorm.DB) {
				statements = append(statements, tx.Statement.SQL.String())
				require.Equal(t, []any{1, 4}, tx.Statement.Vars)
				tx.RowsAffected = 2
			}))
			original := DB
			DB = db
			t.Cleanup(func() { DB = original })
			count, ids, err := DeleteChannelsByExactName("Model")
			require.NoError(t, err)
			require.EqualValues(t, 2, count)
			require.Equal(t, []int{1, 4}, ids)
			require.Len(t, statements, 3)
			require.Contains(t, statements[0], "FOR UPDATE")
		})
	}
}

package model

import (
	"fmt"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func forEachNodeLocDatabase(t *testing.T, test func(*testing.T, *gorm.DB)) {
	t.Helper()
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			prefix := ""
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				if os.Getenv("TEST_MYSQL_DSN") == "" {
					t.Skip("set TEST_MYSQL_DSN to test NodeLoc on MySQL")
				}
				driver = mysql.Open(os.Getenv("TEST_MYSQL_DSN"))
			case "postgres":
				if os.Getenv("TEST_POSTGRES_DSN") == "" {
					t.Skip("set TEST_POSTGRES_DSN to test NodeLoc on PostgreSQL")
				}
				driver = postgres.Open(os.Getenv("TEST_POSTGRES_DSN"))
			}
			if dialect != "sqlite" {
				prefix = fmt.Sprintf("nodeloc_%d_", time.Now().UnixNano())
			}
			db, err := gorm.Open(driver, &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: prefix}})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			if dialect == "sqlite" {
				sqlDB.SetMaxOpenConns(1)
			} else {
				sqlDB.SetMaxOpenConns(8)
			}
			previousDB, previousRedis := DB, common.RedisEnabled
			DB, common.RedisEnabled = db, false
			t.Cleanup(func() {
				DB, common.RedisEnabled = previousDB, previousRedis
				// Only this test's isolated table is removed on external databases.
				require.NoError(t, db.Migrator().DropTable(&User{}))
				require.NoError(t, sqlDB.Close())
			})
			test(t, db)
		})
	}
}

func TestNodeLocColumnDatabaseCompatibility(t *testing.T) {
	// MySQL/PostgreSQL checks compile schema and DML without a server connection.
	for _, dialect := range []gorm.Dialector{
		sqlite.Open("file:nodeloc-dialect?mode=memory&cache=shared"),
		mysql.New(mysql.Config{DSN: "user:pass@tcp(localhost:3306)/test", SkipInitializeWithVersion: true}),
		postgres.New(postgres.Config{DSN: "host=localhost user=test dbname=test"}),
	} {
		t.Run(dialect.Name(), func(t *testing.T) {
			db, err := gorm.Open(dialect, &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true})
			require.NoError(t, err)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })
			stmt := &gorm.Statement{DB: db}
			require.NoError(t, stmt.Parse(&User{}))
			field := stmt.Schema.LookUpField("NodeLocId")
			require.NotNil(t, field)
			require.Equal(t, "nodeloc_id", field.DBName)
			require.Equal(t, "varchar(64)", db.Dialector.DataTypeOf(field))
			require.NotEmpty(t, stmt.Schema.ParseIndexes()["idx_users_node_loc_id"].Fields)
			require.Equal(t, "UNIQUE", stmt.Schema.ParseIndexes()["idx_users_node_loc_id"].Class)
			require.Equal(t, "null", field.DefaultValue)
			for _, result := range []*gorm.DB{
				db.Model(&User{}).Where("id = ?", 1).Update("nodeloc_id", "123"),
				db.Model(&User{}).Where("id = ?", 1).Update("nodeloc_id", nil),
				db.Where("nodeloc_id = ?", "123").Find(&User{}),
			} {
				require.NoError(t, result.Error)
				require.Contains(t, result.Statement.SQL.String(), "nodeloc_id")
			}
		})
	}
}

func TestNodeLocExistingDatabaseMigration(t *testing.T) {
	forEachNodeLocDatabase(t, testNodeLocExistingDatabaseMigration)
}

func testNodeLocExistingDatabaseMigration(t *testing.T, db *gorm.DB) {
	// Reconstruct the full pre-NodeLoc user schema, including required columns.
	userType := reflect.TypeOf(User{})
	var fields []reflect.StructField
	for i := 0; i < userType.NumField(); i++ {
		if field := userType.Field(i); field.Name != "NodeLocId" {
			fields = append(fields, field)
		}
	}
	legacyUser := reflect.New(reflect.StructOf(fields)).Interface()
	table := db.NamingStrategy.TableName("User")
	require.NoError(t, db.Table(table).Migrator().CreateTable(legacyUser))
	reflect.ValueOf(legacyUser).Elem().FieldByName("Username").SetString("existing")
	require.NoError(t, db.Table(table).Create(legacyUser).Error)
	id := int(reflect.ValueOf(legacyUser).Elem().FieldByName("Id").Int())
	require.NoError(t, migrateNodeLocUserID(db))
	require.NoError(t, db.AutoMigrate(&User{}))
	require.True(t, db.Migrator().HasColumn(&User{}, "nodeloc_id"))
	require.True(t, db.Migrator().HasIndex(&User{}, "NodeLocId"))
	var user User
	require.NoError(t, db.First(&user, id).Error)
	require.Equal(t, "existing", user.Username)
	require.Empty(t, user.NodeLocId)
	require.NoError(t, db.Model(&user).Update("nodeloc_id", "123").Error)
	require.NoError(t, migrateNodeLocUserID(db))
	require.NoError(t, db.AutoMigrate(&User{}))
	require.NoError(t, db.First(&user, id).Error)
	require.Equal(t, "123", user.NodeLocId)
	// Unbound users coexist, but even writes that bypass the application check cannot duplicate an ID.
	other := User{Username: "other", AffCode: "OTHER"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&User{Username: "unbound", AffCode: "UNBOUND"}).Error)
	require.Error(t, db.Model(&other).Update("nodeloc_id", "123").Error)
	require.NoError(t, db.Model(&user).Update("nodeloc_id", nil).Error)
	require.NoError(t, db.Model(&other).Update("nodeloc_id", "123").Error)
}

func TestNodeLocBindingPreservesAccountOnDatabase(t *testing.T) {
	forEachNodeLocDatabase(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&User{}))
		user := seedAffQuotaUser(t, "binding", "")
		other := seedAffQuotaUser(t, "other", "")
		legacy := seedAffQuotaUser(t, "legacy", "")
		require.NoError(t, db.Model(&legacy).Update("nodeloc_id", "").Error)
		// The binding request's user snapshot predates another account update.
		expected := user
		expected.Quota += 77
		expected.DisplayName = "changed"
		expected.Email = "changed@example.com"
		require.NoError(t, db.Model(&User{}).Where("id = ?", user.Id).Updates(map[string]any{
			"quota": expected.Quota, "display_name": expected.DisplayName, "email": expected.Email,
		}).Error)
		require.NoError(t, user.UpdateNodeLocId("123"))
		expected.NodeLocId = "123"
		var stored User
		require.NoError(t, db.First(&stored, user.Id).Error)
		require.Equal(t, expected, stored)
		require.Error(t, other.UpdateNodeLocId("123"))
		require.Empty(t, other.NodeLocId)
		require.NoError(t, user.ClearBinding("nodeloc"))
		var count int64
		require.NoError(t, db.Model(&User{}).Where("id IN ? AND nodeloc_id IS NULL", []int{user.Id, other.Id}).Count(&count).Error)
		require.EqualValues(t, 2, count)
		require.NoError(t, other.UpdateNodeLocId("123"))
		require.NoError(t, user.TransferAffQuotaToQuota(user.AffQuota))
		expected.Quota += expected.AffQuota
		expected.AffQuota = 0
		expected.NodeLocId = ""
		stored = User{}
		require.NoError(t, db.First(&stored, user.Id).Error)
		require.Equal(t, expected, stored)
		require.NoError(t, db.Delete(&other).Error)
		require.ErrorIs(t, other.UpdateNodeLocId("456"), gorm.ErrRecordNotFound)
		require.Error(t, (&User{}).UpdateNodeLocId("456"))
		require.Error(t, user.UpdateNodeLocId(""))
	})
}

func TestNodeLocConcurrentQuotaTransfersOnDatabase(t *testing.T) {
	forEachNodeLocDatabase(t, func(t *testing.T, db *gorm.DB) {
		require.NoError(t, db.AutoMigrate(&User{}))
		testTransferAffQuotaConcurrentRequests(t)
	})
}

package model

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

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

func TestNodeLocSQLiteExistingDatabaseMigration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	// Reconstruct the full pre-NodeLoc user schema, including required columns.
	userType := reflect.TypeOf(User{})
	var fields []reflect.StructField
	for i := 0; i < userType.NumField(); i++ {
		if field := userType.Field(i); field.Name != "NodeLocId" {
			fields = append(fields, field)
		}
	}
	legacyUser := reflect.New(reflect.StructOf(fields)).Interface()
	require.NoError(t, db.Table("users").Migrator().CreateTable(legacyUser))
	reflect.ValueOf(legacyUser).Elem().FieldByName("Id").SetInt(1)
	reflect.ValueOf(legacyUser).Elem().FieldByName("Username").SetString("existing")
	require.NoError(t, db.Table("users").Create(legacyUser).Error)
	require.NoError(t, migrateNodeLocUserID(db))
	require.NoError(t, db.AutoMigrate(&User{}))
	require.True(t, db.Migrator().HasColumn(&User{}, "nodeloc_id"))
	require.True(t, db.Migrator().HasIndex(&User{}, "NodeLocId"))
	var user User
	require.NoError(t, db.First(&user, 1).Error)
	require.Equal(t, "existing", user.Username)
	require.Empty(t, user.NodeLocId)
	require.NoError(t, db.Model(&user).Update("nodeloc_id", "123").Error)
	require.NoError(t, migrateNodeLocUserID(db))
	require.NoError(t, db.AutoMigrate(&User{}))
	require.NoError(t, db.First(&user, 1).Error)
	require.Equal(t, "123", user.NodeLocId)
	// Unbound users coexist, but even writes that bypass the application check cannot duplicate an ID.
	other := User{Username: "other", AffCode: "OTHER"}
	require.NoError(t, db.Create(&other).Error)
	require.NoError(t, db.Create(&User{Username: "unbound", AffCode: "UNBOUND"}).Error)
	require.Error(t, db.Model(&other).Update("nodeloc_id", "123").Error)
	require.NoError(t, db.Model(&user).Update("nodeloc_id", nil).Error)
	require.NoError(t, db.Model(&other).Update("nodeloc_id", "123").Error)
}

// Package testutil provides opt-in integration fixtures in disposable databases.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"GoAI/config"
	"github.com/BurntSushi/toml"
	driver "github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func Config(t *testing.T) config.Config {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	t.Chdir(root)
	var conf config.Config
	if _, err := toml.DecodeFile(filepath.Join(root, "config/config.toml"), &conf); err != nil {
		t.Fatal(err)
	}
	return conf
}

// MySQL creates a fresh random database; it never migrates the configured application database.
func MySQL(t *testing.T) *gorm.DB {
	t.Helper()
	if os.Getenv("GOPHERAI_TEST_MYSQL") != "1" {
		t.Skip("set GOPHERAI_TEST_MYSQL=1 to run isolated MySQL integration tests")
	}
	conf := Config(t)
	dsn := driver.NewConfig()
	dsn.User, dsn.Passwd = conf.MysqlUser, conf.MysqlPassword
	dsn.Net, dsn.Addr = "tcp", fmt.Sprintf("%s:%d", conf.MysqlHost, conf.MysqlPort)
	dsn.ParseTime = true
	server, err := gorm.Open(mysql.Open(dsn.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	serverSQL, err := server.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serverSQL.Close() })
	name := "gopherai_test_" + uuid.New().String()
	if err := server.Exec("CREATE DATABASE `" + name + "` CHARACTER SET utf8mb4").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Exec("DROP DATABASE `" + name + "`").Error; err != nil {
			t.Error(err)
		}
	})
	dsn.DBName = name
	db, err := gorm.Open(mysql.Open(dsn.FormatDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

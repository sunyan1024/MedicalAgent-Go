package mysql

import (
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestUIDSchemaReplacesEmptyLegacyUsers(t *testing.T) {
	db := testutil.MySQL(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	if err := db.Exec("CREATE TABLE users (id bigint PRIMARY KEY AUTO_INCREMENT, name varchar(50), username varchar(50))").Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migration(); err != nil {
			t.Fatal(err)
		}
	}
	for _, column := range []string{"id", "name", "username"} {
		if db.Migrator().HasColumn(&model.User{}, column) {
			t.Fatalf("legacy column remains: %s", column)
		}
	}
	accounts := []model.User{{UID: "00123", Nickname: "同名", Email: "a@example.com"}, {UID: "00456", Nickname: "同名", Email: "b@example.com"}}
	if err := db.Create(&accounts).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{UID: "00123", Email: "c@example.com"}).Error; err == nil {
		t.Fatal("duplicate UID accepted")
	}
	if err := db.Create(&model.User{UID: "00789", Email: "a@example.com"}).Error; err == nil {
		t.Fatal("duplicate email accepted")
	}
}

func TestUIDSchemaRejectsPopulatedLegacyUsers(t *testing.T) {
	db := testutil.MySQL(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	if err := db.Exec("CREATE TABLE users (id bigint PRIMARY KEY AUTO_INCREMENT, username varchar(50))").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO users(username) VALUES ('keep')").Error; err != nil {
		t.Fatal(err)
	}
	if err := prepareUIDSchema(); err == nil {
		t.Fatal("populated legacy table accepted")
	}
	var count int64
	if err := db.Table("users").Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("legacy data changed: count=%d err=%v", count, err)
	}
}

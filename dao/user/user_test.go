package user

import (
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestUIDAndNicknameAreIndependent(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	account, ok := Register("00123456789", "uid@example.com", "test-password", " 自定义昵称 ")
	if !ok || account.UID != "00123456789" || account.Nickname != "自定义昵称" {
		t.Fatalf("registration failed: %+v", account)
	}
	for _, identifier := range []string{account.UID, account.Email} {
		found, got := IsExistUserByUIDOrEmail(identifier)
		if !found || got.UID != account.UID || !VerifyPassword(got, "test-password") {
			t.Fatalf("login lookup failed: %s", identifier)
		}
	}
	if found, _ := IsExistUserByUIDOrEmail(account.Nickname); found {
		t.Fatal("nickname accepted as login identifier")
	}
	if err := db.Model(account).Update("nickname", "新昵称").Error; err != nil {
		t.Fatal(err)
	}
	got, err := GetUserByUID(account.UID)
	if err != nil || got.Nickname != "新昵称" {
		t.Fatalf("UID lookup after nickname change: %+v %v", got, err)
	}
}

func TestRegisterDefaultsNicknameToUID(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	for i, nickname := range []string{"", "   "} {
		uid := "0098765432" + string(rune('0'+i))
		account, ok := Register(uid, "default"+string(rune('a'+i))+"@example.com", "test-password", nickname)
		if !ok || account.Nickname != uid {
			t.Fatalf("empty nickname should default to UID: %+v", account)
		}
	}
}

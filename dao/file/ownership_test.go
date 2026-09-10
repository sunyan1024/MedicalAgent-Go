package file

import (
	appmysql "GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"errors"
	"gorm.io/gorm"
	"testing"
)

func TestAccessibleFilesAndScopedDelete(t *testing.T) {
	db := testutil.MySQL(t)
	previous := appmysql.DB
	appmysql.DB = db
	t.Cleanup(func() { appmysql.DB = previous })
	if err := db.AutoMigrate(&model.KnowledgeFile{}); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []model.KnowledgeOwner{model.PersonalKnowledgeOwner("alice"), model.PersonalKnowledgeOwner("bob"), model.EnterpriseKnowledgeOwner("admin")} {
		file := model.KnowledgeFile{ID: owner.Key, Scope: owner.Scope, OwnerKey: owner.Key, UserName: owner.UserName, StoredName: "same.txt", StoredFileID: "same", OriginalName: owner.Key}
		if err := db.Create(&file).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		username string
		count    int
	}{{"alice", 2}, {"new-user", 1}} {
		files, err := GetAccessibleKnowledgeFiles(tc.username)
		if err != nil || len(files) != tc.count {
			t.Fatalf("%s: files=%+v err=%v", tc.username, files, err)
		}
		for _, file := range files {
			if file.Scope == "personal" && file.OwnerKey != tc.username {
				t.Fatal("other user's file accessible")
			}
		}
	}
	alice := model.PersonalKnowledgeOwner("alice")
	if _, err := GetKnowledgeFileByID(alice, "bob"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross-owner read accepted")
	}
	if err := DeleteKnowledgeFile(db, alice, "bob"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("cross-owner delete accepted")
	}
	if err := DeleteKnowledgeFile(db, alice, "alice"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteKnowledgeFile(db, alice, "alice"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatal("repeated delete counted as success")
	}
}

package mysql

import (
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestKnowledgeOwnershipMigration(t *testing.T) {
	db := testutil.MySQL(t)
	previous := DB
	DB = db
	t.Cleanup(func() { DB = previous })
	// Same stored file across two legacy owners is legal and must survive new indexing.
	if err := db.Exec(`CREATE TABLE knowledge_files (
 id varchar(36) PRIMARY KEY, user_name varchar(50) NOT NULL,
 stored_name varchar(255) NOT NULL, original_name varchar(255) NOT NULL,
 stored_file_id varchar(36) NOT NULL, size bigint,
 created_at datetime, updated_at datetime,
 UNIQUE KEY idx_user_stored_name (user_name, stored_name))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO knowledge_files (id,user_name,stored_name,original_name,stored_file_id,size)
 VALUES ('a','alice','same.txt','Alice.txt','shared',1),('b','bob','same.txt','Bob.txt','shared',1)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE users (id bigint PRIMARY KEY AUTO_INCREMENT, username varchar(50))`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO users(username) VALUES ('alice')`).Error; err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migration(); err != nil {
			t.Fatal(err)
		}
	}
	var files []model.KnowledgeFile
	if err := db.Order("id").Find(&files).Error; err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %d", len(files))
	}
	for _, file := range files {
		if file.Scope != model.KnowledgeScopePersonal || file.OwnerKey != file.UserName || file.StoredFileID != "shared" {
			t.Fatalf("wrong migrated ownership: %+v", file)
		}
	}
	var account model.User
	if err := db.First(&account).Error; err != nil {
		t.Fatal(err)
	}
	if account.Role != model.RoleUser {
		t.Fatalf("legacy role = %q", account.Role)
	}
	if db.Migrator().HasIndex(&model.KnowledgeFile{}, "idx_user_stored_name") {
		t.Fatal("legacy unique index remains")
	}
	enterprise := model.KnowledgeFile{ID: "enterprise", Scope: model.KnowledgeScopeEnterprise, OwnerKey: model.EnterpriseOwnerKey, UserName: "alice", StoredName: "same.txt", OriginalName: "Enterprise.txt", StoredFileID: "shared"}
	if err := db.Create(&enterprise).Error; err != nil {
		t.Fatal(err)
	}
	enterprise.ID, enterprise.UserName = "duplicate", "bob"
	if err := db.Create(&enterprise).Error; err == nil {
		t.Fatal("enterprise duplicate across uploaders accepted")
	}
}

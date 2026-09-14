package session

import (
	"GoAI/common/code"
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestSessionModelPersistence(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.Session{}); err != nil {
		t.Fatal(err)
	}
	first, status := CreateStreamSessionOnly("alice", "first", "rag")
	if status != code.CodeSuccess {
		t.Fatal(status)
	}
	second, status := CreateStreamSessionOnly("alice", "second", "qwen")
	if status != code.CodeSuccess {
		t.Fatal(status)
	}
	for _, modelType := range []string{"mcp", "mcp"} {
		if status := SaveSessionModel("alice", first, modelType); status != code.CodeSuccess {
			t.Fatal(status)
		}
	}
	if status := SaveSessionModel("bob", first, "ollama"); status != code.CodeRecordNotFound {
		t.Fatal(status)
	}
	if status := SaveSessionModel("alice", first, "unknown"); status != code.CodeInvalidParams {
		t.Fatal(status)
	}
	sessions, err := GetUserSessionsByUID("alice")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, s := range sessions {
		got[s.SessionID] = s.ModelType
	}
	if got[first] != "mcp" || got[second] != "qwen" {
		t.Fatalf("saved models: %v", got)
	}
	if err := db.Create(&model.Session{ID: "legacy", UID: "legacy"}).Error; err != nil {
		t.Fatal(err)
	}
	var legacy model.Session
	if err := db.First(&legacy, "session_id = ?", "legacy").Error; err != nil {
		t.Fatal(err)
	}
	if legacy.ModelType != "medical_agent" {
		t.Fatal(legacy.ModelType)
	}
}

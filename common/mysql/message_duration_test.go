package mysql

import (
	"GoAI/internal/testutil"
	"GoAI/model"
	"testing"
)

func TestMessageDurationSchema(t *testing.T) {
	db := testutil.MySQL(t)
	for i := 0; i < 2; i++ {
		if err := db.AutoMigrate(&model.Message{}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Exec("INSERT INTO messages (session_id, uid, content, is_user) VALUES (?, ?, ?, ?)", "session", "alice", "answer", false).Error; err != nil {
		t.Fatal(err)
	}
	var message model.Message
	if err := db.First(&message).Error; err != nil {
		t.Fatal(err)
	}
	if message.DurationMS != 0 {
		t.Fatalf("default duration = %d", message.DurationMS)
	}
	if err := db.Exec("UPDATE messages SET duration_ms = NULL").Error; err == nil {
		t.Fatal("duration_ms must not accept NULL")
	}
}

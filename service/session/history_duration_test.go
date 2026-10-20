package session

import (
	"context"
	"testing"

	"GoAI/common/aihelper"
	"GoAI/common/code"
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"github.com/cloudwego/eino/schema"
)

type historyDurationModel struct{}

func (*historyDurationModel) GenerateResponse(context.Context, []*schema.Message) (*schema.Message, error) {
	panic("history restoration must not invoke a model")
}
func (*historyDurationModel) StreamResponse(context.Context, []*schema.Message, aihelper.StreamCallback) (string, error) {
	panic("history restoration must not invoke a model")
}
func (*historyDurationModel) GetModelType() string { return "history-duration-test" }

func TestHistoryDurationFromDatabaseAndRestoredMemory(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.Message{}, &model.MedicalRun{}); err != nil {
		t.Fatal(err)
	}
	zero, duration := int64(0), int64(65000)
	messages := []model.Message{
		{SessionID: "duration-history", UID: "alice", Content: "question", IsUser: true},
		{SessionID: "duration-history", UID: "alice", Content: "fast answer", DurationMS: zero},
		{SessionID: "duration-history", UID: "alice", Content: "answer", DurationMS: duration},
		{SessionID: "duration-history", UID: "bob", Content: "private answer", DurationMS: duration},
	}
	if err := db.Create(&messages).Error; err != nil {
		t.Fatal(err)
	}
	check := func(t *testing.T) {
		t.Helper()
		history, status := GetChatHistory("alice", "duration-history")
		if status != code.CodeSuccess || len(history) != 3 {
			t.Fatalf("history: %+v status=%v", history, status)
		}
		if history[0].DurationMS != 0 || history[1].DurationMS != 0 || history[2].DurationMS != duration {
			t.Fatalf("historical durations changed: %+v", history)
		}
	}
	t.Run("database", check)
	aihelper.GetGlobalFactory().RegisterModel("history-duration-test", func(context.Context, map[string]interface{}) (aihelper.AIModel, error) {
		return &historyDurationModel{}, nil
	})
	manager := aihelper.GetGlobalManager()
	helper, err := manager.GetOrCreateAIHelper("alice", "duration-history", "history-duration-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { manager.RemoveAIHelper("alice", "duration-history") })
	for i := 0; i < 3; i++ {
		helper.AppendMessage(&messages[i], false)
	}
	t.Run("restored_memory", check)
	if err := db.Create(&model.MedicalRun{ID: "run", UID: "alice", SessionID: "duration-history"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Run("medical_memory_history", check)
	helper.AppendMessage(&model.Message{SessionID: "duration-history", UID: "alice", Content: "pending answer", DurationMS: 123}, false)
	history, status := GetChatHistory("alice", "duration-history")
	if status != code.CodeSuccess || len(history) != 4 || history[3].Content != "pending answer" || history[3].DurationMS != 123 {
		t.Fatalf("medical history ignored pending in-memory message: %+v %v", history, status)
	}
}

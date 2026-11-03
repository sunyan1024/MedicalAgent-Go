package aihelper

import (
	"context"
	"errors"
	"strings"
	"testing"

	"GoAI/internal/testutil"
	"GoAI/model"
	"gorm.io/gorm"
)

func TestSummaryMySQLCacheAndRecovery(t *testing.T) {
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}); err != nil {
		t.Fatal(err)
	}
	a, _ := summaryHistory(t, 16)
	repo := mysqlSummaryRepository{db: db}
	a.summaryStore = repo
	if err := db.Create(&model.Session{ID: a.SessionID, UID: "alice"}).Error; err != nil {
		t.Fatal(err)
	}
	all := a.GetMessages()
	if err := db.Create(all[:len(all)-1]).Error; err != nil {
		t.Fatal(err)
	}
	loads := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:summary_loads", func(tx *gorm.DB) {
		if tx.Statement.Table == "sessions" && strings.Contains(tx.Statement.SQL.String(), "context_summary") {
			loads++
		}
	}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	a.summarize = func(context.Context, string, []*model.Message) (string, error) {
		calls++
		return "父亲的症状已更正", nil
	}
	first, err := a.ContextMessages(context.Background())
	if err != nil || calls != 1 || loads != 1 {
		t.Fatalf("generation calls=%d loads=%d err=%v", calls, loads, err)
	}
	a.ContextMessages(context.Background())
	if calls != 1 || loads != 1 {
		t.Fatal("ordinary turn queried summary or recompressed")
	}
	restored := NewAIHelper(nil, a.SessionID)
	restored.summaryStore = repo
	restored.summarize = a.summarize
	var records []*model.Message
	if err := db.Where("session_id = ? AND uid = ?", a.SessionID, "alice").Order("message_id").Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	for _, m := range records {
		restored.AppendMessage(m, false)
	}
	restored.AppendMessage(all[len(all)-1], false)
	out, err := restored.ContextMessages(context.Background())
	if err != nil || loads != 2 || calls != 1 || first[0].Content != out[0].Content {
		t.Fatal("durable recovery failed")
	}
	if _, err := repo.Load(context.Background(), "bob", a.SessionID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("ownership load: %v", err)
	}
	ids := []string{all[0].StableID, all[1].StableID}
	if ready, err := repo.Ready(context.Background(), "bob", a.SessionID, ids); err != nil || ready {
		t.Fatal("cross-user messages considered durable")
	}
	ids = append(ids, "pending-id")
	if ready, err := repo.Ready(context.Background(), "alice", a.SessionID, ids); err != nil || ready {
		t.Fatal("pending message considered durable")
	}
	// Duplicate deliveries cannot hide a missing ID in the batched check.
	duplicate := *all[0]
	duplicate.ID = 0
	if err := db.Create(&duplicate).Error; err != nil {
		t.Fatal(err)
	}
	if ready, err := repo.Ready(context.Background(), "alice", a.SessionID, ids); err != nil || ready {
		t.Fatal("duplicate row hid missing ID")
	}
	state, err := repo.Load(context.Background(), "alice", a.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != 1 || state.Through == "" {
		t.Fatalf("bad checkpoint: %+v", state)
	}
	if saved, err := repo.Save(context.Background(), "alice", a.SessionID, summaryState{}, summaryState{Text: "stale", Version: 1}); err != nil || saved {
		t.Fatal("stale version overwrite")
	}
	if saved, err := repo.Save(context.Background(), "bob", a.SessionID, state, summaryState{Text: "private", Version: 2}); err != nil || saved {
		t.Fatal("cross-user overwrite")
	}
}

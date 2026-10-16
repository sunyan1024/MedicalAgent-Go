package medical

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"gorm.io/gorm"
)

func TestEventPersistenceAndPublicProjection(t *testing.T) {
	db := testutil.MySQL(t)
	old := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = old })
	if err := db.AutoMigrate(&model.MedicalRun{}, &model.MedicalRunEvent{}); err != nil {
		t.Fatal(err)
	}
	run := model.MedicalRun{ID: "run", UID: "alice", SessionID: "session", Status: "running"}
	if err := db.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	w := newEventWriter(db)
	secret := strings.Repeat("完整原始内容", 1000)
	e := core.Event{ID: "event", RunID: run.ID, Sequence: 1, Type: "tool_start", Tool: "search", Input: secret, Summary: "摘要", OccurredAt: time.Now().UTC()}
	e.Sources = []core.Source{{ID: "web_test", Kind: "web", URL: "https://example.org/evidence", Title: "网页", Content: "证据"}, {ID: "knowledge_" + strings.Repeat("a", 64) + "_1", Kind: "knowledge", Title: "知识库资料", KnowledgeScope: "personal"}}
	// Already-canceled browser context must not affect the independent writer.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	w.append(e)
	var row model.MedicalRunEvent
	if err := db.First(&row, "event_id = ?", e.ID).Error; err != nil {
		t.Fatal(err)
	}
	if row.Input != secret || row.Sequence != 1 || row.OccurredAt.IsZero() {
		t.Fatal("event not committed during execution")
	}
	if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error { return tx.Model(&run).Update("status", "completed").Error }); err == nil {
		t.Fatal("canceled terminal transaction succeeded")
	}
	rows, err := Runs(context.Background(), "alice", "session")
	if err != nil || len(rows) != 1 {
		t.Fatal(rows, err)
	}
	var restored []core.Event
	if err := json.Unmarshal([]byte(rows[0].Events), &restored); err != nil || len(restored) != 1 || len(restored[0].Sources) != 2 || restored[0].Sources[0] != e.Sources[0] || restored[0].Sources[1] != e.Sources[1] {
		t.Fatal("source history mismatch", err)
	}
	body, _ := json.Marshal(rows)
	if strings.Contains(string(body), secret) || !strings.Contains(rows[0].Events, "摘要") {
		t.Fatal("invalid public projection")
	}
	if other, err := Runs(context.Background(), "bob", "session"); err != nil || len(other) != 0 {
		t.Fatal("ownership", other, err)
	}
	// An uncertain commit can be retried without duplication.
	w.append(e)
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&model.MedicalRunEvent{}).Count(&count)
	if count != 1 {
		t.Fatal("duplicate event", count)
	}
}

func TestEventWriteFailureAndRetry(t *testing.T) {
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.MedicalRunEvent{}); err != nil {
		t.Fatal(err)
	}
	blocked := true
	attempts := 0
	if err := db.Callback().Create().Before("gorm:create").Register("test:event_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "medical_run_events" {
			attempts++
			if blocked {
				tx.AddError(errors.New("private driver failure"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	w := newEventWriter(db)
	for i, id := range []string{"one", "two"} {
		w.append(core.Event{ID: id, RunID: "run", Sequence: int64(i + 1), Type: "tool_start", OccurredAt: time.Now().UTC()})
	}
	if attempts != 1 {
		t.Fatal("writes did not pause", attempts)
	}
	if err := w.finish(); err == nil || strings.Contains(err.Error(), "private") {
		t.Fatal("missing or unsafe persistence error", err)
	}
	blocked = false
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	if err := w.finish(); err != nil {
		t.Fatal(err)
	}
	var rows []model.MedicalRunEvent
	if err := db.Order("sequence").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "one" || rows[1].ID != "two" {
		t.Fatal(rows)
	}
}

func TestRunEventsSurviveDisconnectAndTerminalFailure(t *testing.T) {
	for _, terminalFailure := range []bool{false, true} {
		name := "disconnect"
		if terminalFailure {
			name = "terminal_failure"
		}
		t.Run(name, func(t *testing.T) {
			db := testutil.MySQL(t)
			oldDB, oldRuntime := mysql.DB, newRuntime
			mysql.DB = db
			t.Cleanup(func() { mysql.DB = oldDB; newRuntime = oldRuntime })
			if err := db.AutoMigrate(&model.MedicalReport{}, &model.MedicalRun{}, &model.MedicalRunEvent{}, &model.Session{}, &model.Message{}, &model.UserMemory{}); err != nil {
				t.Fatal(err)
			}
			newRuntime = func(_ context.Context, id string, uid string, _ func(context.Context) ([]core.Indicator, error), emit func(core.Event)) (*core.Runtime, error) {
				return core.NewRuntime(id, core.Dependencies{Model: &followupModel{}, Emit: emit}), nil
			}
			if terminalFailure {
				if err := db.Callback().Create().Before("gorm:create").Register("test:terminal_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "messages" {
						tx.AddError(errors.New("test terminal failure"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runID := ""
			observed := 0
			_, err := Run(ctx, "alice", ChatRequest{Question: "测试事件"}, func(e core.Event) {
				runID = e.RunID
				observed++
				var count int64
				if queryErr := db.Model(&model.MedicalRunEvent{}).Where("event_id = ?", e.ID).Count(&count).Error; queryErr != nil || count != 1 {
					t.Errorf("SSE preceded commit: %d %v", count, queryErr)
				}
				if !terminalFailure && e.Type == "run_start" {
					cancel()
				}
			})
			if terminalFailure && (err == nil || !strings.Contains(err.Error(), "结果持久化失败")) {
				t.Fatal(err)
			}
			if !terminalFailure && err != nil {
				t.Fatal(err)
			}
			var rows []model.MedicalRunEvent
			if err := db.Where("run_id = ?", runID).Order("sequence").Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if len(rows) != observed || len(rows) < 2 || rows[len(rows)-1].Type != "run_end" {
				t.Fatal("terminal events missing", len(rows), observed)
			}
			var run model.MedicalRun
			if err := db.First(&run, "run_id = ?", runID).Error; err != nil {
				t.Fatal(err)
			}
			if terminalFailure && run.Status != "running" {
				t.Fatal("terminal transaction did not roll back", run.Status)
			}
		})
	}
}

func TestSourceEventRecordFormat(t *testing.T) {
	sources := []core.Source{
		{Kind: "knowledge", ID: "knowledge_" + strings.Repeat("a", 64) + "_1", Title: "知识库", Content: "证据"},
		{Kind: "web", ID: "web_" + strings.Repeat("b", 64), URL: "https://example.org", Title: "网页", Content: "证据"},
	}
	output := core.JSON(sources)
	e := core.Event{Type: "tool_end", Sources: sources, Output: output, Summary: output}
	row := eventRecord(e)
	if row.Sources != output || row.Output != output || row.Summary != output {
		t.Fatalf("source fields differ: %+v", row)
	}
}

package medical

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type followupModel struct{ sawHistory bool }

func (m *followupModel) Generate(_ context.Context, msgs []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "持续三天") {
			m.sawHistory = true
		}
	}
	return schema.AssistantMessage("请问是否伴随其他症状？", nil), nil
}
func (m *followupModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, msgs, opts...)
	if e != nil {
		return nil, e
	}
	text := []rune(v.Content)
	middle := len(text) / 2
	return schema.StreamReaderFromArray([]*schema.Message{
		schema.AssistantMessage(string(text[:middle]), nil),
		schema.AssistantMessage(string(text[middle:]), nil),
	}), nil
}

func TestMedicalOwnershipConfirmationAndPersistentFollowup(t *testing.T) {
	db := testutil.MySQL(t)
	installImmediateChatConsumer(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.MedicalReport{}, &model.MedicalRun{}, &model.MedicalRunEvent{}, &model.Session{}, &model.Message{}, &model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	report := model.MedicalReport{ID: "report-a", UID: "alice", Status: "ready", Indicators: "[]"}
	if err := db.Create(&report).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := GetReport(ctx, "bob", report.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user read accepted", err)
	}
	items := []core.Indicator{{Name: "指标", Value: "2", Unit: "U", Reference: "1-3"}}
	if _, err := ConfirmReport(ctx, "bob", report.ID, items); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user confirmation accepted", err)
	}
	if _, err := Run(ctx, "alice", ChatRequest{Question: "解读", ReportID: report.ID}, nil); err == nil {
		t.Fatal("unconfirmed report accepted")
	}
	if _, err := ConfirmReport(ctx, "alice", report.ID, items); err != nil {
		t.Fatal(err)
	}
	m := &followupModel{}
	previousRuntime := newRuntime
	t.Cleanup(func() { newRuntime = previousRuntime })
	newRuntime = func(_ context.Context, id string, uid string, read func(context.Context) ([]core.Indicator, error), emit func(core.Event)) (*core.Runtime, error) {
		if uid != "alice" {
			t.Fatalf("runtime received wrong user: %q", uid)
		}
		if read == nil {
			t.Fatal("confirmed report lost")
		}
		snapshot, err := read(ctx)
		if err != nil || len(snapshot) != 1 {
			t.Fatal(snapshot, err)
		}
		return core.NewRuntime(id, core.Dependencies{Model: m, ReadReport: read, Emit: emit}), nil
	}
	var liveEvents []core.Event
	run, err := Run(ctx, "alice", ChatRequest{Question: "症状持续三天", ReportID: report.ID}, func(e core.Event) {
		liveEvents = append(liveEvents, e)
	})
	if err != nil {
		t.Fatal(err)
	}
	var displayed string
	var durableEvents []core.Event
	deltas, finals := 0, 0
	for _, e := range liveEvents {
		if e.Type != "answer_delta" {
			durableEvents = append(durableEvents, e)
		}
		switch e.Type {
		case "answer_delta":
			deltas++
			displayed += e.Delta
		case "answer_final":
			finals++
			displayed = e.Delta
		}
	}
	if deltas != 2 || finals != 1 || displayed != run.Answer {
		t.Fatalf("stream/result mismatch: deltas=%d finals=%d displayed=%q answer=%q", deltas, finals, displayed, run.Answer)
	}
	var savedDeltas int64
	if err := db.Model(&model.MedicalRunEvent{}).Where("run_id = ? AND type = ?", run.ID, "answer_delta").Count(&savedDeltas).Error; err != nil || savedDeltas != 0 {
		t.Fatalf("answer fragments persisted: %d %v", savedDeltas, err)
	}
	var savedRun model.MedicalRun
	if err := db.First(&savedRun, "run_id = ?", run.ID).Error; err != nil || savedRun.Answer != displayed {
		t.Fatalf("saved answer mismatch: %+v %v", savedRun, err)
	}
	var savedMessage model.Message
	if err := db.Where("session_id = ? AND is_user = ?", run.SessionID, false).First(&savedMessage).Error; err != nil || savedMessage.Content != displayed {
		t.Fatalf("saved message mismatch: %+v %v", savedMessage, err)
	}
	if savedMessage.DurationMS < 0 {
		t.Fatalf("saved reply duration missing: %+v", savedMessage)
	}
	var userMessage model.Message
	if err := db.Where("session_id = ? AND is_user = ?", run.SessionID, true).First(&userMessage).Error; err != nil || userMessage.DurationMS != 0 {
		t.Fatalf("unexpected user message duration: %+v %v", userMessage, err)
	}
	restored, err := Runs(ctx, "alice", run.SessionID)
	if err != nil || len(restored) != 1 {
		t.Fatal(restored, err)
	}
	var restoredEvents []core.Event
	if err := json.Unmarshal([]byte(restored[0].Events), &restoredEvents); err != nil {
		t.Fatal(err)
	}
	if len(restoredEvents) != len(durableEvents) {
		t.Fatal("durable events lost in history")
	}
	for i, e := range restoredEvents {
		if e.Type != durableEvents[i].Type || e.Delta != durableEvents[i].Delta {
			t.Fatalf("history event mismatch at %d", i)
		}
	}
	var savedEvents []model.MedicalRunEvent
	if err := db.Where("run_id = ?", run.ID).Order("sequence").Find(&savedEvents).Error; err != nil {
		t.Fatal(err)
	}
	for i, e := range savedEvents {
		if e.Sequence != int64(i+1) {
			t.Fatalf("non-contiguous saved sequence: %+v", e)
		}
	}
	if _, err = Run(ctx, "bob", ChatRequest{Question: "看看", SessionID: run.SessionID}, nil); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-user session accepted", err)
	}
	if _, err = Run(ctx, "alice", ChatRequest{Question: "继续", SessionID: run.SessionID}, nil); err != nil {
		t.Fatal(err)
	}
	if !m.sawHistory {
		t.Fatal("history not restored")
	}
	runs, err := Runs(ctx, "alice", run.SessionID)
	if err != nil || len(runs) != 2 {
		t.Fatal(runs, err)
	}
	if got, err := Runs(ctx, "bob", run.SessionID); err != nil || len(got) != 0 {
		t.Fatal("cross-user traces exposed")
	}
	if _, err = Run(ctx, "alice", ChatRequest{Question: "新会话", ReportID: report.ID}, nil); err == nil {
		t.Fatal("report reused across sessions")
	}
	var count int64
	db.Model(&model.Message{}).Count(&count)
	if count != 4 {
		t.Fatalf("messages=%d", count)
	}
}

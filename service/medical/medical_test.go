package medical

import (
	"context"
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
	return schema.StreamReaderFromArray([]*schema.Message{v}), e
}

func TestMedicalOwnershipConfirmationAndPersistentFollowup(t *testing.T) {
	db := testutil.MySQL(t)
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
	run, err := Run(ctx, "alice", ChatRequest{Question: "症状持续三天", ReportID: report.ID}, nil)
	if err != nil {
		t.Fatal(err)
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

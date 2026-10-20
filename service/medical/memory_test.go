package medical

import (
	"context"
	"strings"
	"testing"

	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	memoryservice "GoAI/service/memory"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type memoryProbe struct{ instruction string }

func (m *memoryProbe) Generate(_ context.Context, msgs []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.instruction = msgs[0].Content
	return schema.AssistantMessage("已读取背景", nil), nil
}
func (m *memoryProbe) Stream(ctx context.Context, msgs []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := m.Generate(ctx, msgs, opts...)
	return schema.StreamReaderFromArray([]*schema.Message{v}), e
}
func TestMemoryAcrossMedicalSessionsAndClear(t *testing.T) {
	db := testutil.MySQL(t)
	installImmediateChatConsumer(t)
	oldDB, oldRuntime := mysql.DB, newRuntime
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = oldDB; newRuntime = oldRuntime })
	if err := db.AutoMigrate(&model.UserMemory{}, &model.Session{}, &model.Message{}, &model.MedicalReport{}, &model.MedicalRun{}, &model.MedicalRunEvent{}); err != nil {
		t.Fatal(err)
	}
	probe := &memoryProbe{}
	newRuntime = func(_ context.Context, id string, uid string, read func(context.Context) ([]core.Indicator, error), emit func(core.Event)) (*core.Runtime, error) {
		return core.NewRuntime(id, core.Dependencies{Model: probe, ReadReport: read, Emit: emit}), nil
	}
	ctx := context.Background()
	store := memoryservice.New(db)
	content := "# 本人健康背景\n用户自述青霉素过敏\n# 家属情况\n父亲有高血压"
	if _, err := store.Update(ctx, "alice", content, 0); err != nil {
		t.Fatal(err)
	}
	a, err := Run(ctx, "alice", ChatRequest{Question: "我曾告诉你过敏史"}, nil)
	if err != nil || !strings.Contains(probe.instruction, "用户自述青霉素过敏") {
		t.Fatal(err, probe.instruction)
	}
	b, err := Run(ctx, "alice", ChatRequest{Question: "新会话"}, nil)
	if err != nil || a.SessionID == b.SessionID || !strings.Contains(probe.instruction, "父亲有高血压") {
		t.Fatal(err)
	}
	if _, err = Run(ctx, "bob", ChatRequest{Question: "新用户"}, nil); err != nil || strings.Contains(probe.instruction, "青霉素过敏") {
		t.Fatal("other user memory exposed", err)
	}
	if _, err = store.Update(ctx, "alice", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err = Run(ctx, "alice", ChatRequest{Question: "继续", SessionID: a.SessionID}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(probe.instruction, "青霉素过敏") || !strings.Contains(probe.instruction, `"version":2`) || !strings.Contains(probe.instruction, "不根据旧聊天自动恢复") {
		t.Fatal("cleared memory or deletion policy incorrect")
	}
	if err = db.Migrator().DropTable(&model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	if _, err = Run(ctx, "alice", ChatRequest{Question: "记忆服务故障仍可回答"}, nil); err != nil || !strings.Contains(probe.instruction, "长期记忆暂不可用") {
		t.Fatal(err)
	}
}

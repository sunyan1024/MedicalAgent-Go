package medical

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"GoAI/common/aihelper"
	"GoAI/common/code"
	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/common/rabbitmq"
	"GoAI/internal/testutil"
	"GoAI/model"
	sessionservice "GoAI/service/session"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/streadway/amqp"
	"gorm.io/gorm"
)

type chatProbe struct {
	kind   string
	inputs [][]*schema.Message
}

func (m *chatProbe) GetModelType() string { return m.kind }
func (m *chatProbe) GenerateResponse(_ context.Context, msgs []*schema.Message) (*schema.Message, error) {
	m.inputs = append(m.inputs, msgs)
	return schema.AssistantMessage("ordinary answer", nil), nil
}
func (m *chatProbe) StreamResponse(ctx context.Context, msgs []*schema.Message, cb aihelper.StreamCallback) (string, error) {
	reply, err := m.GenerateResponse(ctx, msgs)
	if err == nil {
		cb(reply.Content)
	}
	return reply.Content, err
}

// Use the production manager and model-switching path, substituting only model
// construction and message transport. Each test releases its helper instances.
func installChatHelpers(t *testing.T, save func(*model.Message) (*model.Message, error)) *chatProbe {
	t.Helper()
	probe := &chatProbe{kind: "qwen"}
	factory := aihelper.GetGlobalFactory()
	factory.RegisterModel("qwen", func(context.Context, map[string]interface{}) (aihelper.AIModel, error) {
		return probe, nil
	})
	factory.RegisterModel("medical_agent", func(context.Context, map[string]interface{}) (aihelper.AIModel, error) {
		return &chatProbe{kind: "medical_agent"}, nil
	})
	old := getHelper
	keys := make(map[[2]string]bool)
	getHelper = func(uid, sid string) (*aihelper.AIHelper, error) {
		helper, err := old(uid, sid)
		if err == nil {
			keys[[2]string{uid, sid}] = true
			helper.SetSaveFunc(save)
		}
		return helper, err
	}
	t.Cleanup(func() {
		getHelper = old
		for key := range keys {
			aihelper.GetGlobalManager().RemoveAIHelper(key[0], key[1])
		}
		factory.RegisterModel("qwen", func(ctx context.Context, _ map[string]interface{}) (aihelper.AIModel, error) {
			return aihelper.NewQwenModel(ctx)
		})
		factory.RegisterModel("medical_agent", func(ctx context.Context, _ map[string]interface{}) (aihelper.AIModel, error) {
			return aihelper.NewMedicalModel(ctx)
		})
	})
	return probe
}

// Existing persistence tests exercise the actual consumer with an immediate
// transport; delayed consumption is covered separately below.
func installImmediateChatConsumer(t *testing.T) {
	t.Helper()
	installChatHelpers(t, func(msg *model.Message) (*model.Message, error) {
		payload := rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UID, msg.IsUser, msg.DurationMS)
		return msg, rabbitmq.MQMessage(&amqp.Delivery{Body: payload})
	})
}

type historyProbe struct{ inputs [][]*schema.Message }

func (m *historyProbe) Generate(_ context.Context, msgs []*schema.Message, _ ...einomodel.Option) (*schema.Message, error) {
	m.inputs = append(m.inputs, msgs)
	return schema.AssistantMessage("medical answer", nil), nil
}
func (m *historyProbe) Stream(ctx context.Context, msgs []*schema.Message, opts ...einomodel.Option) (*schema.StreamReader[*schema.Message], error) {
	reply, err := m.Generate(ctx, msgs, opts...)
	return schema.StreamReaderFromArray([]*schema.Message{reply}), err
}

func historyDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := testutil.MySQL(t)
	oldDB, oldRuntime := mysql.DB, newRuntime
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = oldDB; newRuntime = oldRuntime })
	if err := db.AutoMigrate(&model.Session{}, &model.Message{}, &model.MedicalReport{}, &model.MedicalRun{}, &model.MedicalRunEvent{}, &model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestModelSwitchSharesFullHistoryBeforeQueueConsumption(t *testing.T) {
	for _, medicalFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("medical_first_%t", medicalFirst), func(t *testing.T) {
			db := historyDB(t)
			var pending [][]byte
			save := func(msg *model.Message) (*model.Message, error) {
				pending = append(pending, rabbitmq.GenerateMessageMQParam(msg.SessionID, msg.Content, msg.UID, msg.IsUser, msg.DurationMS))
				return msg, nil
			}
			ordinary := installChatHelpers(t, save)
			probe := &historyProbe{}
			newRuntime = func(_ context.Context, id, uid string, read func(context.Context) ([]core.Indicator, error), emit func(core.Event)) (*core.Runtime, error) {
				return core.NewRuntime(id, core.Dependencies{Model: probe, ReadReport: read, Emit: emit}), nil
			}
			ctx := context.Background()
			var sid string
			var want []string
			if !medicalFirst {
				sid = "switch-history"
				if err := db.Create(&model.Session{ID: sid, UID: "alice", ModelType: "qwen"}).Error; err != nil {
					t.Fatal(err)
				}
				helper, err := aihelper.GetGlobalManager().GetOrCreateAIHelper("alice", sid, "qwen", nil)
				if err != nil {
					t.Fatal(err)
				}
				helper.SetSaveFunc(save)
				for i := 0; i < 12; i++ {
					question := fmt.Sprintf("ordinary question %d", i)
					if _, err := helper.GenerateResponse("alice", ctx, question); err != nil {
						t.Fatal(err)
					}
					want = append(want, question, "ordinary answer")
				}
			}
			run, err := Run(ctx, "alice", ChatRequest{SessionID: sid, Question: "medical question"}, nil)
			if err != nil {
				t.Fatal(err)
			}
			sid = run.SessionID
			want = append(want, "medical question")
			assertChatInput(t, probe.inputs[0], want)
			want = append(want, run.Answer)
			history, status := sessionservice.GetChatHistory("alice", sid)
			if status != code.CodeSuccess || len(history) != len(want) {
				t.Fatalf("history missing while queue delayed: %v %v", history, status)
			}
			for i := range history {
				if history[i].Content != want[i] {
					t.Fatalf("history[%d]=%q want %q", i, history[i].Content, want[i])
				}
			}
			if _, err := Run(ctx, "bob", ChatRequest{SessionID: sid, Question: "private"}, nil); !errors.Is(err, ErrNotFound) {
				t.Fatalf("cross-user access: %v", err)
			}
			helper, err := aihelper.GetGlobalManager().GetOrCreateAIHelper("alice", sid, "qwen", nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := helper.StreamResponse("alice", ctx, func(string) {}, "back to ordinary"); err != nil {
				t.Fatal(err)
			}
			want = append(want, "back to ordinary")
			assertChatInput(t, ordinary.inputs[len(ordinary.inputs)-1], want)
			want = append(want, "ordinary answer")
			var count int64
			if err := db.Model(&model.Message{}).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("chat messages bypassed queue: %d %v", count, err)
			}
			if len(pending) != len(want) {
				t.Fatalf("published %d messages, want %d", len(pending), len(want))
			}
			for _, payload := range pending {
				if err := rabbitmq.MQMessage(&amqp.Delivery{Body: payload}); err != nil {
					t.Fatal(err)
				}
			}
			var stored []model.Message
			if err := db.Order("message_id").Find(&stored).Error; err != nil || len(stored) != len(want) {
				t.Fatalf("consumed history: %d %v", len(stored), err)
			}
			for i := range stored {
				if stored[i].Content != want[i] {
					t.Fatalf("stored[%d]=%q want %q", i, stored[i].Content, want[i])
				}
			}
			if len(helper.GetMessages()) != len(want) {
				t.Fatal("consumer duplicated in-memory messages")
			}
		})
	}
}

func assertChatInput(t *testing.T, msgs []*schema.Message, want []string) {
	t.Helper()
	var got []string
	for _, msg := range msgs {
		if msg.Role == schema.User || msg.Role == schema.Assistant {
			got = append(got, msg.Content)
		}
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("model input:\n%v\nwant:\n%v", got, want)
	}
}

func TestMedicalPublishFailureKeepsMemoryAndRunState(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("publish_%d", failAt), func(t *testing.T) {
			db := historyDB(t)
			published := 0
			installChatHelpers(t, func(msg *model.Message) (*model.Message, error) {
				published++
				if published == failAt {
					return msg, errors.New("transport unavailable")
				}
				return msg, nil
			})
			probe := &historyProbe{}
			newRuntime = func(_ context.Context, id, uid string, _ func(context.Context) ([]core.Indicator, error), emit func(core.Event)) (*core.Runtime, error) {
				return core.NewRuntime(id, core.Dependencies{Model: probe, Emit: emit}), nil
			}
			_, err := Run(context.Background(), "alice", ChatRequest{Question: "question"}, nil)
			if err == nil || !strings.Contains(err.Error(), "消息保存失败") {
				t.Fatalf("missing publication failure: %v", err)
			}
			var run model.MedicalRun
			if err := db.First(&run).Error; err != nil {
				t.Fatal(err)
			}
			helper, ok := aihelper.GetGlobalManager().GetAIHelper("alice", run.SessionID)
			if !ok || len(helper.GetMessages()) != failAt || published != failAt {
				t.Fatal("memory lost or publication retried")
			}
			if failAt == 1 && (run.Status != "failed" || len(probe.inputs) != 0) {
				t.Fatal("generation continued after question publication failed")
			}
			if failAt == 2 && (run.Status != "completed" || run.Answer != "medical answer") {
				t.Fatalf("terminal result lost: %+v", run)
			}
		})
	}
}

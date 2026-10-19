package aihelper

import (
	"context"
	"errors"
	"testing"
	"time"

	"GoAI/model"
	"github.com/cloudwego/eino/schema"
)

type durationModel struct{ fail bool }

func (m *durationModel) GenerateResponse(context.Context, []*schema.Message) (*schema.Message, error) {
	// Simulate time spent retrieving evidence and generating the answer.
	time.Sleep(5 * time.Millisecond)
	if m.fail {
		return nil, errors.New("generation failed")
	}
	return schema.AssistantMessage("answer", nil), nil
}
func (m *durationModel) StreamResponse(ctx context.Context, messages []*schema.Message, cb StreamCallback) (string, error) {
	msg, err := m.GenerateResponse(ctx, messages)
	if err != nil {
		return "", err
	}
	cb(msg.Content)
	return msg.Content, nil
}
func (*durationModel) GetModelType() string { return "duration-test" }

func TestReplyDurationSurvivesMemoryAndSaveCallback(t *testing.T) {
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "streaming"}[streaming], func(t *testing.T) {
			helper := NewAIHelper(&durationModel{}, "session")
			var saved []*model.Message
			helper.SetSaveFunc(func(msg *model.Message) (*model.Message, error) {
				saved = append(saved, msg)
				return msg, nil
			})
			var answer *model.Message
			var err error
			if streaming {
				answer, err = helper.StreamResponse("alice", context.Background(), func(string) {}, "question")
			} else {
				answer, err = helper.GenerateResponse("alice", context.Background(), "question")
			}
			if err != nil {
				t.Fatal(err)
			}
			if answer.DurationMS < 5 {
				t.Fatalf("missing processing duration: %+v", answer)
			}
			messages := helper.GetMessages()
			if len(saved) != 2 || len(messages) != 2 || saved[1] != answer || messages[1] != answer {
				t.Fatal("answer metadata lost in memory/save callback")
			}
			if saved[0].DurationMS != 0 || messages[0].DurationMS != 0 {
				t.Fatal("user message should not have a reply duration")
			}
		})
	}
}

func TestRestoreMessagePreservesZeroDurationWithoutSaving(t *testing.T) {
	helper := NewAIHelper(&durationModel{}, "session")
	helper.SetSaveFunc(func(*model.Message) (*model.Message, error) {
		t.Fatal("restoring a message must not save it again")
		return nil, nil
	})
	zero := int64(0)
	helper.AppendMessage(&model.Message{Content: "answer", DurationMS: zero}, false)
	got := helper.GetMessages()[0]
	if got.DurationMS != 0 {
		t.Fatal("zero duration lost on restore")
	}
}

func TestFailedGenerationDoesNotPersistAnAssistantMessage(t *testing.T) {
	helper := NewAIHelper(&durationModel{fail: true}, "session")
	helper.SetSaveFunc(func(msg *model.Message) (*model.Message, error) { return msg, nil })
	if _, err := helper.GenerateResponse("alice", context.Background(), "question"); err == nil {
		t.Fatal("expected generation error")
	}
	if messages := helper.GetMessages(); len(messages) != 1 || !messages[0].IsUser {
		t.Fatal("failed generation should not introduce a new assistant record")
	}
}

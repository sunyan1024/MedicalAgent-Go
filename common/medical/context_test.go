package medical

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// legacyContext reconstructs the pre-change input at the model boundary. Only
// tests use it; tools, budgets and all other runtime behavior are shared.
func legacyContext(msgs []*schema.Message) []*schema.Message {
	if len(msgs) == 0 || !strings.Contains(msgs[0].Content, mainInstruction) {
		return msgs
	}
	out := append([]*schema.Message(nil), msgs...)
	for i := 1; i < len(out); i++ {
		if out[i].Role != schema.System || !(strings.HasPrefix(out[i].Content, "\n本轮长期记忆数据（JSON）：") || out[i].Content == "\n长期记忆暂不可用。") {
			continue
		}
		first := *out[0]
		first.Content = strings.Replace(first.Content, memoryInstruction, memoryInstruction+out[i].Content, 1)
		out[0] = &first
		return append(out[:i], out[i+1:]...)
	}
	return out
}

type inputProbe struct {
	messages []*schema.Message
	options  []model.Option
}

func (p *inputProbe) Generate(_ context.Context, m []*schema.Message, o ...model.Option) (*schema.Message, error) {
	p.messages = m
	p.options = o
	return schema.AssistantMessage("已收到", nil), nil
}
func (p *inputProbe) Stream(ctx context.Context, m []*schema.Message, o ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	v, e := p.Generate(ctx, m, o...)
	return schema.StreamReaderFromArray([]*schema.Message{v}), e
}

func TestTurnBackgroundOrderingAndIsolation(t *testing.T) {
	for _, report := range []bool{false, true} {
		for _, state := range []string{"present", "empty", "unavailable"} {
			t.Run(state+JSON(report), func(t *testing.T) {
				history := []*schema.Message{schema.UserMessage("历史问题"), schema.AssistantMessage("历史回答", nil)}
				if report {
					history = append(history, schema.SystemMessage("当前会话有用户已确认的报告，可委派 report_analyst 读取。报告内容不得从历史推测。"))
				}
				history = append(history, schema.UserMessage("当前问题"))
				original := append([]*schema.Message(nil), history...)
				probe := &inputProbe{}
				r := NewRuntime("context-test", Dependencies{Model: probe, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) {
					if state == "unavailable" {
						return MemoryDocument{}, errors.New("offline")
					}
					doc := MemoryDocument{Version: 1}
					if state == "present" {
						doc.ContentMarkdown = "偏好简洁中文"
					}
					return doc, nil
				}}})
				_, err := r.Run(context.Background(), history)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(history, original) {
					t.Fatal("caller history changed")
				}
				m := probe.messages
				if strings.Contains(m[0].Content, "本轮长期记忆数据") || strings.Contains(m[0].Content, `"version"`) {
					t.Fatal("dynamic memory in fixed instruction")
				}
				for i := 0; i < len(history)-1; i++ {
					if m[i+1] != history[i] {
						t.Fatal("history prefix changed")
					}
				}
				if m[len(m)-2].Content != "当前问题" || m[len(m)-3].Role != schema.System {
					t.Fatal("background must precede question and budget")
				}
				old := legacyContext(m)
				if len(old) != len(m)-1 || !strings.Contains(old[0].Content, m[len(m)-3].Content) {
					t.Fatal("legacy reconstruction failed")
				}
			})
		}
	}
}

func TestMemoryChangePreservesPrefix(t *testing.T) {
	history := []*schema.Message{schema.UserMessage("旧问题"), schema.AssistantMessage("旧回答", nil), schema.UserMessage("新问题")}
	a := withTurnBackground(history, "memory v1")
	b := withTurnBackground(history, "memory v2")
	if !reflect.DeepEqual(a[:2], b[:2]) || a[2].Content == b[2].Content {
		t.Fatal("prefix invariant")
	}

	var captures [][]*schema.Message
	for version := int64(1); version <= 2; version++ {
		probe := &inputProbe{}
		r := NewRuntime("prefix", Dependencies{Model: probe, Memory: &MemoryAccess{Read: func(context.Context) (MemoryDocument, error) {
			return MemoryDocument{ContentMarkdown: JSON(version), Version: version}, nil
		}}})
		if _, err := r.Run(context.Background(), history); err != nil {
			t.Fatal(err)
		}
		captures = append(captures, probe.messages)
	}
	if !reflect.DeepEqual(captures[0][:3], captures[1][:3]) {
		t.Fatal("actual model system/history prefix changed with memory")
	}
	if reflect.DeepEqual(legacyContext(captures[0])[0], legacyContext(captures[1])[0]) {
		t.Fatal("legacy input must change before history")
	}
	for _, input := range [][]*schema.Message{nil, {schema.AssistantMessage("answer", nil)}} {
		out := withTurnBackground(input, "background")
		if out[len(out)-1].Content != "background" {
			t.Fatal("missing fallback background")
		}
	}
}

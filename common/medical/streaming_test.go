package medical

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type answerStreamModel struct {
	scenarioModel
	chunks     []*schema.Message
	beforeLast <-chan struct{}
	streamErr  error
	delegate   bool
	mainCalls  int
}

func (m *answerStreamModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if !strings.Contains(msgs[0].Content, mainInstruction) {
		return m.scenarioModel.Stream(ctx, msgs, opts...)
	}
	m.mainCalls++
	if m.delegate && m.mainCalls == 1 {
		return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("工具内部说明", []schema.ToolCall{{
			ID: "delegate", Type: "function", Function: schema.FunctionCall{Name: "medical_researcher", Arguments: `{"request":"查找证据"}`},
		}})}), nil
	}
	reader, writer := schema.Pipe[*schema.Message](0)
	go func() {
		defer writer.Close()
		for i, chunk := range m.chunks {
			if i == len(m.chunks)-1 && m.beforeLast != nil {
				select {
				case <-m.beforeLast:
				case <-ctx.Done():
					return
				}
			}
			if writer.Send(chunk, nil) {
				return
			}
		}
		if m.streamErr != nil {
			writer.Send(nil, m.streamErr)
		}
	}()
	return reader, nil
}

func TestAnswerDeltaArrivesBeforeModelFinishes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release := make(chan struct{}, 1)
	defer close(release)
	deltas := make(chan string, 4)
	r := NewRuntime("streaming", Dependencies{
		Model: &answerStreamModel{beforeLast: release, chunks: []*schema.Message{
			{Role: schema.Assistant, ReasoningContent: "不应公开的推理"},
			schema.AssistantMessage("结论[know", nil),
			schema.AssistantMessage("ledge_example]。", nil),
		}},
		Emit: func(e Event) {
			if e.Type == "answer_delta" {
				deltas <- e.Delta
			}
		},
	})
	done := make(chan struct{})
	var answer string
	var runErr error
	go func() {
		defer close(done)
		answer, runErr = r.Run(ctx, []*schema.Message{schema.UserMessage("解释指标")})
	}()
	select {
	case delta := <-deltas:
		if delta != "结论[know" {
			t.Fatalf("unexpected first delta: %q", delta)
		}
	case <-ctx.Done():
		t.Fatal("no delta received while the model was waiting to finish")
	}
	select {
	case <-done:
		t.Fatal("model finished before its final chunk was released")
	default:
	}
	// Release the last chunk without closing the channel twice during cleanup.
	release <- struct{}{}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("agent did not finish")
	}
	if runErr != nil || answer != "结论。" {
		t.Fatalf("answer=%q err=%v", answer, runErr)
	}
	assertFinalAnswer(t, r.Events, answer, "completed")
}

func TestAnswerFinalReplacesDeltasAndFallbacks(t *testing.T) {
	for _, tc := range []struct {
		name      string
		chunks    []*schema.Message
		streamErr error
		want      string
		partial   bool
	}{
		{name: "single chunk", chunks: []*schema.Message{schema.AssistantMessage("完整回答", nil)}, want: "完整回答"},
		{name: "empty", chunks: []*schema.Message{schema.AssistantMessage("", nil)}, want: "本次未获得可用结果，请补充问题或重试。", partial: true},
		{name: "only markers", chunks: []*schema.Message{schema.AssistantMessage("[web_example]", nil)}, want: "本次未获得可用结果，请补充问题或重试。", partial: true},
		{name: "stream failure", chunks: []*schema.Message{schema.AssistantMessage("未完成的正文", nil)}, streamErr: io.ErrUnexpectedEOF, partial: true},
		{name: "cancellation", chunks: []*schema.Message{schema.AssistantMessage("未完成的正文", nil)}, streamErr: context.Canceled, partial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRuntime(tc.name, Dependencies{Model: &answerStreamModel{chunks: tc.chunks, streamErr: tc.streamErr}})
			answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("提问")})
			if (err != nil) != tc.partial || (tc.want != "" && answer != tc.want) {
				t.Fatalf("answer=%q err=%v", answer, err)
			}
			if tc.streamErr != nil && !errors.Is(err, tc.streamErr) {
				t.Fatalf("lost stream error: %v", err)
			}
			status := "completed"
			if tc.partial {
				status = "partial"
			}
			assertFinalAnswer(t, r.Events, answer, status)
		})
	}
}

func TestAnswerStreamDoesNotExposeToolsOrChildJSON(t *testing.T) {
	var deltas []string
	r := NewRuntime("delegation-stream", Dependencies{
		Search: func(context.Context, string) ([]Source, error) {
			return []Source{{ID: "stream", Content: "证据"}}, nil
		},
		Model: &answerStreamModel{
			delegate: true,
			chunks:   []*schema.Message{schema.AssistantMessage("第一段", nil), schema.AssistantMessage("第二段", nil)},
		},
		Emit: func(e Event) {
			if e.Type == "answer_delta" {
				deltas = append(deltas, e.Delta)
			}
		},
	})
	answer, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("查找证据")})
	if err != nil {
		t.Fatal(err)
	}
	childSeen := false
	for _, e := range r.Events {
		if e.Type == "answer_delta" {
			t.Fatal("transient fragment retained in run history")
		}
		if e.Type == "subagent_end" {
			childSeen = true
		}
	}
	if !childSeen || len(deltas) != 2 || strings.Join(deltas, "") != "第一段第二段" {
		t.Fatalf("child=%v deltas=%q", childSeen, deltas)
	}
	assertFinalAnswer(t, r.Events, answer, "completed")
}

func assertFinalAnswer(t *testing.T, events []Event, answer, status string) {
	t.Helper()
	finals := 0
	for i, e := range events {
		if e.Type != "answer_final" {
			continue
		}
		finals++
		if e.Delta != answer || i != len(events)-2 || events[i+1].Type != "run_end" || events[i+1].Status != status {
			t.Fatalf("final answer and terminal state mismatch: %+v", events[i:])
		}
	}
	if finals != 1 {
		t.Fatalf("want one answer_final, got %d", finals)
	}
}

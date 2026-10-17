package medical

import (
	"context"
	"errors"
	"github.com/cloudwego/eino/components/tool"
	"strings"
	"testing"
)

func TestCompleteEventBodiesAndOrdering(t *testing.T) {
	r := NewRuntime("run", Dependencies{})
	local := 0
	raw := "{invalid:" + strings.Repeat("输入", 1000)
	output := strings.Repeat("输出", 1000)
	failure := errors.New(strings.Repeat("错误", 1000))
	task := r.bounded("child", "delegation", &local, toolInfo("test", "", ""), func(context.Context, string, string) (string, error) { return output, failure }).(tool.InvokableTool)
	if _, err := task.InvokableRun(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	r.used = 9
	if _, err := task.InvokableRun(context.Background(), raw); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	if len(r.Events) != 3 {
		t.Fatal(len(r.Events))
	}
	start, end, rejected := r.Events[0], r.Events[1], r.Events[2]
	if start.Input != raw || end.Output != output || end.Error != failure.Error() || end.ParentID != start.ID || start.ParentID != "delegation" || rejected.Input != raw || rejected.Status != "rejected" {
		t.Fatal("lost event details")
	}
	if len([]rune(start.Summary)) > 300 || len([]rune(end.Summary)) > 400 {
		t.Fatal("unbounded summaries")
	}
	for i, e := range r.Events {
		if e.Sequence != int64(i+1) || e.OccurredAt.IsZero() {
			t.Fatal("missing event ordering")
		}
		body := JSON(e)
		if strings.Contains(body, raw) || strings.Contains(body, output) || strings.Contains(body, failure.Error()) {
			t.Fatal("raw body leaked")
		}
	}
}

func TestAnswerFragmentsDoNotConsumeEventSequence(t *testing.T) {
	var received []Event
	r := NewRuntime("run", Dependencies{Emit: func(e Event) { received = append(received, e) }})
	r.emit(Event{Type: "run_start"})
	for i := 0; i < 1000; i++ {
		r.emit(Event{Type: "answer_delta", Delta: "片段"})
		if i == 499 {
			r.emit(Event{Type: "sources"})
		}
	}
	r.emit(Event{Type: "answer_final", Delta: "完整答案"})
	r.emit(Event{Type: "run_end"})
	if len(received) != 1004 || len(r.Events) != 4 {
		t.Fatalf("received=%d durable=%d", len(received), len(r.Events))
	}
	for i, e := range r.Events {
		if e.Sequence != int64(i+1) || e.Type == "answer_delta" {
			t.Fatalf("durable sequence inflated: %+v", e)
		}
	}
	for _, e := range received {
		if e.ID == "" || e.OccurredAt.IsZero() || e.RunID != r.RunID {
			t.Fatalf("event metadata lost: %+v", e)
		}
		if e.Type == "answer_delta" && e.Sequence != 0 {
			t.Fatalf("transient fragment consumed sequence: %+v", e)
		}
	}
	if r.Events[2].Type != "answer_final" || r.Events[2].Sequence != 3 {
		t.Fatal("unexpected final answer sequence", r.Events[2])
	}
}

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

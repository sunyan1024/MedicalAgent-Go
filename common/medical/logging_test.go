package medical

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// These tests change the standard logger and must not run in parallel.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	out, flags, prefix := log.Writer(), log.Flags(), log.Prefix()
	log.SetOutput(&buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(out)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	return &buf
}

func TestRuntimeLogsDelegationWithoutCallback(t *testing.T) {
	buf := captureLogs(t)
	r := NewRuntime("logged-run", Dependencies{
		Model: &scenarioModel{},
		Search: func(context.Context, string) ([]Source, error) {
			return []Source{{ID: "med_abc123", Title: "测试", Content: "证据"}}, nil
		},
	})
	if _, err := r.Run(context.Background(), []*schema.Message{schema.UserMessage("什么是血常规")}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	index := 0
	starts := make(map[string]Event)
	for _, e := range r.Events {
		switch e.Type {
		case "run_start", "run_end", "subagent_start", "subagent_end", "tool_start", "tool_end":
		default:
			continue
		}
		if index >= len(lines) {
			t.Fatal("missing log", e.Type)
		}
		for _, field := range []string{
			`[MedicalAgent]`, `run_id="logged-run"`,
			"event_id=" + strconv.Quote(e.ID), "parent_id=" + strconv.Quote(e.ParentID),
			"type=" + strconv.Quote(e.Type), "agent=" + strconv.Quote(e.Agent),
			"tool=" + strconv.Quote(e.Tool), "status=" + strconv.Quote(e.Status),
			fmt.Sprintf("duration_ms=%d", e.DurationMS),
		} {
			if !strings.Contains(lines[index], field) {
				t.Fatalf("missing %s in %s", field, lines[index])
			}
		}
		if e.Type == "tool_start" && starts[e.ParentID].Type != "subagent_start" {
			t.Fatal("tool log has no preceding delegation")
		}
		if e.Type == "tool_end" || e.Type == "subagent_end" {
			if starts[e.ParentID].Tool != e.Tool {
				t.Fatal("end log has no matching start")
			}
		}
		starts[e.ID] = e
		index++
	}
	if index != len(lines) || !strings.Contains(buf.String(), `type="tool_start"`) {
		t.Fatal("unexpected logs", buf.String())
	}
}

func TestToolFailureAndRejectionLogs(t *testing.T) {
	buf := captureLogs(t)
	r := NewRuntime("failure", Dependencies{})
	local, calls := 0, 0
	failure := errors.New(strings.Repeat("错", 450) + "\nerror tail")
	task := r.bounded("child", "delegation", &local, toolInfo("test", "", ""), func(context.Context, string, string) (string, error) {
		calls++
		return "", failure
	}).(tool.InvokableTool)
	out, err := task.InvokableRun(context.Background(), `{}`)
	if err != nil || !strings.Contains(out, "error tail") {
		t.Fatalf("observation changed: %s %v", out, err)
	}
	r.used = 9
	if _, err = task.InvokableRun(context.Background(), `{}`); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	r.used = 1
	local = 3
	if _, err = task.InvokableRun(context.Background(), `{}`); !errors.Is(err, ErrBudget) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = task.InvokableRun(ctx, `{}`); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls != 1 || r.used != 1 || local != 3 || len(r.Events) != 5 {
		t.Fatal("rejection changed execution, budget or events")
	}
	logs := buf.String()
	if strings.Count(logs, `type="tool_rejected"`) != 3 || !strings.Contains(logs, `status="failed"`) ||
		!strings.Contains(logs, "context canceled") || !strings.Contains(logs, ErrBudget.Error()) || strings.Contains(logs, "error tail") {
		t.Fatal(logs)
	}
}

func TestLogSummaryBoundsAndExcludedBodies(t *testing.T) {
	buf := captureLogs(t)
	for _, tc := range []struct {
		kind  string
		limit int
	}{{"tool_start", 300}, {"subagent_start", 300}, {"tool_end", 400}, {"subagent_end", 400}} {
		buf.Reset()
		logEvent(Event{Type: tc.kind, Summary: "首行\n" + strings.Repeat("文", 500)})
		line := buf.String()
		if strings.Count(line, "\n") != 1 {
			t.Fatal("multiline log", line)
		}
		_, quoted, ok := strings.Cut(strings.TrimSpace(line), "summary=")
		summary, err := strconv.Unquote(quoted)
		if !ok || err != nil || len([]rune(summary)) != tc.limit || !strings.HasSuffix(summary, "…") {
			t.Fatalf("invalid summary: %s", line)
		}
	}
	buf.Reset()
	r := NewRuntime("excluded", Dependencies{})
	r.emit(Event{Type: "answer_delta", Delta: "answer secret"})
	r.emit(Event{Type: "sources", Sources: []Source{{Content: "source secret"}}})
	if buf.Len() != 0 || len(r.Events) != 2 {
		t.Fatal("excluded events should only be retained")
	}
}

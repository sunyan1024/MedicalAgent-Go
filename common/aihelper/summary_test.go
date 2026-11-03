package aihelper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"GoAI/model"
)

type summaryFakeStore struct {
	state                      summaryState
	loads, saves, checks       int
	loadErr, saveErr, readyErr error
	pending, conflict          bool
	onReady                    func([]string)
}

func (s *summaryFakeStore) Load(context.Context, string, string) (summaryState, error) {
	s.loads++
	return s.state, s.loadErr
}
func (s *summaryFakeStore) Ready(_ context.Context, _ string, _ string, ids []string) (bool, error) {
	s.checks++
	if s.onReady != nil {
		s.onReady(ids)
	}
	return !s.pending, s.readyErr
}
func (s *summaryFakeStore) Save(_ context.Context, _ string, _ string, old, next summaryState) (bool, error) {
	s.saves++
	if s.saveErr != nil {
		return false, s.saveErr
	}
	if s.conflict {
		return false, nil
	}
	if s.state.Version != old.Version {
		return false, nil
	}
	s.state = next
	return true, nil
}

func summaryHistory(t *testing.T, turns int) (*AIHelper, *summaryFakeStore) {
	t.Helper()
	a := NewAIHelper(nil, "summary-session")
	store := &summaryFakeStore{}
	a.summaryStore = store
	for i := 0; i < turns; i++ {
		a.AppendMessage(&model.Message{SessionID: a.SessionID, UID: "alice", Content: fmt.Sprintf("question-%d ", i) + strings.Repeat("x", 1500), IsUser: true}, false)
		a.AppendMessage(&model.Message{SessionID: a.SessionID, UID: "alice", Content: fmt.Sprintf("answer-%d", i)}, false)
	}
	a.AppendMessage(&model.Message{SessionID: a.SessionID, UID: "alice", Content: "current", IsUser: true}, false)
	return a, store
}
func TestSummarySingleBatchCacheAndProgress(t *testing.T) {
	a, store := summaryHistory(t, 16)
	all := a.GetMessages()
	all[0].Content = strings.Repeat("长", 21000)
	all[2].Content = "更正：是我父亲，不是我"
	calls := 0
	batchEnd := 0
	a.summarize = func(ctx context.Context, previous string, msgs []*model.Message) (string, error) {
		calls++
		batchEnd = len(msgs)
		if previous != "" || len(msgs) < 2 || textSize(msgs) <= 20000 {
			t.Fatalf("batch truncated: %d", len(msgs))
		}
		for i, m := range msgs {
			if m != all[i] {
				t.Fatal("batch reordered")
			}
		}
		if !strings.Contains(msgs[2].Content, "父亲") {
			t.Fatal("correction missing")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 30*time.Second {
			t.Fatal("missing total deadline")
		}
		return "父亲的背景摘要", nil
	}
	out, err := a.ContextMessages(context.Background())
	if err != nil || calls != 1 || store.checks != 1 || store.saves != 1 || store.loads != 1 {
		t.Fatalf("calls=%d store=%+v err=%v", calls, store, err)
	}
	if store.state.Through != all[batchEnd-1].StableID || store.state.Version != 1 || len(out) != len(all)-batchEnd+1 || !strings.Contains(out[0].Content, "父亲") {
		t.Fatalf("wrong state/window: %+v len=%d", store.state, len(out))
	}
	if len(a.GetMessages()) != 33 {
		t.Fatal("archive modified")
	}
	a.ContextMessages(context.Background())
	if calls != 1 || store.loads != 1 {
		t.Fatal("cached summary reloaded or repeated")
	}
	// A rebuilt helper reloads durable state, without another summary generation.
	restored := NewAIHelper(nil, a.SessionID)
	restored.summaryStore = store
	for _, msg := range all {
		restored.AppendMessage(msg, false)
	}
	restored.summarize = func(context.Context, string, []*model.Message) (string, error) {
		t.Fatal("recompressed covered messages")
		return "", nil
	}
	replay, err := restored.ContextMessages(context.Background())
	if err != nil || store.loads != 2 || replay[0].Content != out[0].Content {
		t.Fatal("restart restoration failed")
	}
}
func TestSummaryEmptyCacheAndLoadRetry(t *testing.T) {
	a, store := summaryHistory(t, 1)
	a.ContextMessages(context.Background())
	a.ContextMessages(context.Background())
	if store.loads != 1 || !a.summary.loaded {
		t.Fatal("empty summary not cached")
	}
	a, store = summaryHistory(t, 16)
	store.loadErr = errors.New("unavailable")
	calls := 0
	a.summarize = func(context.Context, string, []*model.Message) (string, error) { calls++; return "new", nil }
	a.ContextMessages(context.Background())
	if a.summary.loaded || calls != 0 {
		t.Fatal("failed load used as empty version")
	}
	store.loadErr = nil
	a.ContextMessages(context.Background())
	if store.loads != 2 || calls != 1 || !a.summary.loaded {
		t.Fatal("load not retried")
	}
}
func TestSummaryFailuresNeverAdvance(t *testing.T) {
	for _, kind := range []string{"pending", "check_error", "model_error", "timeout", "empty", "too_long", "save_error"} {
		t.Run(kind, func(t *testing.T) {
			a, s := summaryHistory(t, 17)
			s.state = summaryState{Text: "old", Through: a.GetMessages()[1].StableID, Version: 3}
			if kind == "pending" {
				s.pending = true
			}
			if kind == "check_error" {
				s.readyErr = errors.New("query failed")
			}
			if kind == "save_error" {
				s.saveErr = errors.New("save failed")
			}
			calls := 0
			a.summarize = func(context.Context, string, []*model.Message) (string, error) {
				calls++
				switch kind {
				case "model_error":
					return "", errors.New("provider failed")
				case "timeout":
					return "", context.DeadlineExceeded
				case "empty":
					return " ", nil
				case "too_long":
					return strings.Repeat("长", 10000), nil
				}
				return "new", nil
			}
			out, err := a.ContextMessages(context.Background())
			if err != nil || s.state.Text != "old" || a.summary.state != s.state {
				t.Fatalf("advanced on failure: %+v", s.state)
			}
			if len(out) == 0 || !strings.Contains(out[0].Content, "old") {
				t.Fatal("old summary lost")
			}
			if (kind == "pending" || kind == "check_error") && calls != 0 {
				t.Fatal("generated before persistence")
			}
		})
	}
}
func TestSummaryConflictRefresh(t *testing.T) {
	for _, refreshFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(refreshFailure), func(t *testing.T) {
			a, s := summaryHistory(t, 16)
			s.conflict = true
			calls := 0
			a.summarize = func(context.Context, string, []*model.Message) (string, error) {
				calls++
				s.state = summaryState{Text: "other writer", Through: a.GetMessages()[13].StableID, Version: 2}
				if refreshFailure {
					s.loadErr = errors.New("refresh failed")
				}
				return "uncommitted", nil
			}
			out, _ := a.ContextMessages(context.Background())
			if calls != 1 || s.loads != 2 || a.summary.loaded == refreshFailure {
				t.Fatal("conflict not refreshed/invalidated")
			}
			if !refreshFailure && (a.summary.state != s.state || !strings.Contains(out[0].Content, "other writer") || out[1].Content != a.GetMessages()[14].Content) {
				t.Fatal("stale state used")
			}
			if refreshFailure {
				s.loadErr = nil
				s.conflict = false
				a.ContextMessages(context.Background())
				if s.loads != 3 || calls != 1 {
					t.Fatal("invalid cache not reloaded")
				}
			}
		})
	}
}
func TestSummaryCacheIsolation(t *testing.T) {
	a, s := summaryHistory(t, 1)
	s.state = summaryState{Text: "alice private"}
	a.ContextMessages(context.Background())
	// Even an incorrectly reused helper may not expose its old user's cached state.
	all := a.GetMessages()
	for _, m := range all {
		m.UID = "bob"
	}
	s.state = summaryState{Text: "bob background"}
	out, _ := a.ContextMessages(context.Background())
	if s.loads != 2 || strings.Contains(out[0].Content, "alice private") {
		t.Fatal("cross-user cache reused")
	}
	a.ChangeModelType(&durationModel{})
	a.ContextMessages(context.Background())
	if s.loads != 2 {
		t.Fatal("model switch discarded cache")
	}
}

func TestSummaryConcurrentContextCalls(t *testing.T) {
	a, s := summaryHistory(t, 16)
	calls := 0
	a.summarize = func(context.Context, string, []*model.Message) (string, error) {
		calls++
		time.Sleep(time.Millisecond)
		return "shared summary", nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.ContextMessages(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.loads != 1 || calls != 1 || s.saves != 1 {
		t.Fatalf("concurrent duplication loads=%d calls=%d saves=%d", s.loads, calls, s.saves)
	}
}
func TestSummaryPendingBatchRetriesAsWhole(t *testing.T) {
	a, s := summaryHistory(t, 16)
	s.pending = true
	calls := 0
	a.summarize = func(_ context.Context, _ string, msgs []*model.Message) (string, error) {
		calls++
		if len(msgs) == 0 || len(msgs)%2 != 0 {
			t.Fatal("partial batch")
		}
		return "complete", nil
	}
	a.ContextMessages(context.Background())
	if calls != 0 || s.saves != 0 {
		t.Fatal("pending batch advanced")
	}
	s.pending = false
	a.ContextMessages(context.Background())
	if calls != 1 || s.saves != 1 || s.checks != 2 {
		t.Fatal("pending batch was not retried")
	}
}

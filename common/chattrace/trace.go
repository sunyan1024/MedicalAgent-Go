package chattrace

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"log"
	"sync"
	"time"
)

type key struct{}
type Trace struct {
	mu     sync.Mutex
	ID     string
	start  time.Time
	stages map[string]int64
}

func Start(ctx context.Context) (context.Context, *Trace) {
	t := &Trace{ID: uuid.NewString(), start: time.Now(), stages: map[string]int64{}}
	return context.WithValue(ctx, key{}, t), t
}
func Mark(ctx context.Context, stage string) {
	t, _ := ctx.Value(key{}).(*Trace)
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.stages[stage]; !exists {
		t.stages[stage] = time.Since(t.start).Milliseconds()
	}
}
func (t *Trace) Finish(status string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"trace_id": t.ID, "model_type": "qwen", "status": status, "elapsed_ms": time.Since(t.start).Milliseconds(), "stages_ms": t.stages})
	log.Printf("[ChatTiming] %s", b)
}

package medical

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	core "GoAI/common/medical"
	"GoAI/common/mysql"
	"GoAI/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// RunResponse preserves the public trace format without storing a JSON snapshot.
type RunResponse struct {
	model.MedicalRun
	Events string `json:"eventsJson"`
}

type eventWriter struct {
	mu      sync.Mutex
	db      *gorm.DB
	pending []model.MedicalRunEvent
	failed  bool
}

func newEventWriter(db *gorm.DB) *eventWriter {
	// GORM's SQL logger can interpolate parameters on errors/slow queries. Event
	// bodies must never be written to ordinary logs, including database errors.
	return &eventWriter{db: db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})}
}

func eventRecord(e core.Event) model.MedicalRunEvent {
	return model.MedicalRunEvent{ID: e.ID, RunID: e.RunID, Sequence: e.Sequence,
		ParentID: e.ParentID, Type: e.Type, Agent: e.Agent, Tool: e.Tool, Status: e.Status,
		OccurredAt: e.OccurredAt, DurationMS: e.DurationMS, Summary: e.Summary,
		Input: e.Input, Output: e.Output, Error: e.Error, Sources: core.JSON(e.Sources), Delta: e.Delta}
}

func (w *eventWriter) append(e core.Event) {
	w.mu.Lock()
	defer w.mu.Unlock()
	row := eventRecord(e)
	w.pending = append(w.pending, row)
	if w.failed {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error; err != nil {
		w.failed = true
		log.Printf("[MedicalAgent] event persistence failed run_id=%q event_id=%q; retry at run end", e.RunID, e.ID)
		return
	}
	w.pending = w.pending[:0]
}

func (w *eventWriter) finish() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := w.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(w.pending, 50).Error; err != nil {
		// Do not propagate driver error strings: they may contain raw event values.
		log.Printf("[MedicalAgent] event persistence retry failed run_id=%q pending=%d", w.pending[0].RunID, len(w.pending))
		return fmt.Errorf("%d 条执行事件未确认保存", len(w.pending))
	}
	w.pending = nil
	w.failed = false
	return nil
}

func loadRunResponses(ctx context.Context, runs []model.MedicalRun) ([]RunResponse, error) {
	responses := make([]RunResponse, 0, len(runs))
	if len(runs) == 0 {
		return responses, nil
	}
	ids := make([]string, 0, len(runs))
	for _, r := range runs {
		ids = append(ids, r.ID)
	}
	var rows []model.MedicalRunEvent
	// Raw inputs, outputs and errors never enter the public response path.
	if err := mysql.DB.WithContext(ctx).Select("event_id", "run_id", "sequence", "parent_id", "type", "agent", "tool", "status", "summary", "duration_ms", "sources", "delta").Where("run_id IN ?", ids).Order("run_id, sequence").Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make(map[string][]core.Event)
	for _, row := range rows {
		e := core.Event{ID: row.ID, RunID: row.RunID, ParentID: row.ParentID, Type: row.Type, Agent: row.Agent, Tool: row.Tool, Status: row.Status, Summary: row.Summary, DurationMS: row.DurationMS, Delta: row.Delta}
		if row.Sources != "" {
			if err := json.Unmarshal([]byte(row.Sources), &e.Sources); err != nil {
				return nil, fmt.Errorf("事件 %s 的来源数据无法读取", row.ID)
			}
		}
		events[row.RunID] = append(events[row.RunID], e)
	}
	for _, r := range runs {
		trace := events[r.ID]
		if trace == nil {
			trace = []core.Event{}
		}
		responses = append(responses, RunResponse{MedicalRun: r, Events: core.JSON(trace)})
	}
	return responses, nil
}

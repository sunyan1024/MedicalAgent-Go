package medical

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Indicator struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Unit      string `json:"unit"`
	Reference string `json:"reference"`
	Source    string `json:"source"`
}

type Finding struct {
	Indicator
	Status string `json:"status"`
}

var numericRange = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*(?:-|~|～|至|—|–)\s*(-?\d+(?:\.\d+)?)\s*$`)

// Compare uses only the user's confirmed report interval in the same unit.
// Qualitative values and ranges with units/operators are deliberately left for review.
func Compare(items []Indicator) []Finding {
	results := make([]Finding, 0, len(items))
	for _, item := range items {
		f := Finding{Indicator: item, Status: "待核对"}
		v, err := strconv.ParseFloat(strings.TrimSpace(item.Value), 64)
		r := numericRange.FindStringSubmatch(item.Reference)
		if err == nil && !math.IsNaN(v) && !math.IsInf(v, 0) && len(r) == 3 && strings.TrimSpace(item.Unit) != "" {
			low, _ := strconv.ParseFloat(r[1], 64)
			high, _ := strconv.ParseFloat(r[2], 64)
			if low <= high {
				f.Status = "范围内"
				if v < low {
					f.Status = "偏低"
				}
				if v > high {
					f.Status = "偏高"
				}
			}
		}
		results = append(results, f)
	}
	return results
}

func ValidateIndicators(items []Indicator) error {
	if len(items) == 0 || len(items) > 100 {
		return fmt.Errorf("请确认 1～100 个报告指标")
	}
	for _, i := range items {
		if strings.TrimSpace(i.Name) == "" || strings.TrimSpace(i.Value) == "" {
			return fmt.Errorf("指标名称和结果不能为空")
		}
		if len(i.Name) > 200 || len(i.Value) > 100 || len(i.Unit) > 100 || len(i.Reference) > 200 || len(i.Source) > 2000 {
			return fmt.Errorf("指标字段过长")
		}
	}
	return nil
}

type Source struct {
	Kind    string `json:"kind"`
	URL     string `json:"url,omitempty"`
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type Event struct {
	Sequence   int64     `json:"-"`
	OccurredAt time.Time `json:"-"`
	Input      string    `json:"-"`
	Output     string    `json:"-"`
	Error      string    `json:"-"`
	SessionID  string    `json:"sessionId,omitempty"`
	RunID      string    `json:"runId"`
	ID         string    `json:"id"`
	ParentID   string    `json:"parentId,omitempty"`
	Type       string    `json:"type"`
	Agent      string    `json:"agent,omitempty"`
	Tool       string    `json:"tool,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	DurationMS int64     `json:"durationMs,omitempty"`
	Status     string    `json:"status,omitempty"`
	Sources    []Source  `json:"sources,omitempty"`
	Delta      string    `json:"delta,omitempty"`
}

func JSON(v any) string { b, _ := json.Marshal(v); return string(b) }

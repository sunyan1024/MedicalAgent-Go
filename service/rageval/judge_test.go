package rageval

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type stubJudgeModel struct {
	content string
	err     error
}

func (s stubJudgeModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &schema.Message{Content: s.content}, nil
}

func TestParseJudgeScore(t *testing.T) {
	got, err := parseJudgeScore(`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"supported"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Score != 4 || !got.Pass || got.FaithfulnessScore != 5 || got.AnswerRelevanceScore != 4 || got.ContextPrecisionScore != 3 || got.Reason != "supported" {
		t.Fatalf("unexpected score: %+v", got)
	}
}

func TestParseJudgeScoreRejectsInvalidResponses(t *testing.T) {
	tests := []string{
		`not json`,
		"```json\n{\"score\":4,\"pass\":true,\"faithfulness_score\":5,\"answer_relevance_score\":4,\"context_precision_score\":3,\"reason\":\"bad wrapper\"}\n```",
		`{"score":6,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"out of range"}`,
		`{"score":4.5,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"decimal"}`,
		`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"reason":"missing context score"}`,
		`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":""}`,
		`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"","extra":1}`,
		`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"ok"} trailing`,
		`{"score":4,"pass":true,"faithfulness_score":5,"answer_relevance_score":4,"context_precision_score":3,"reason":"ok"} {}`,
	}
	for _, input := range tests {
		if _, err := parseJudgeScore(input); err == nil {
			t.Fatalf("expected error for %s", input)
		}
	}
}

func TestRunJudgeReturnsModelError(t *testing.T) {
	_, err := runJudgeWithTimeout(context.Background(), stubJudgeModel{err: errors.New("unavailable")}, EvalCaseRequest{Question: "q", ExpectedAnswer: "a"}, "answer", nil, time.Second)
	if err == nil {
		t.Fatal("expected model error")
	}
}

func TestJudgeWarningAcceptsModelPassAndReportsConflict(t *testing.T) {
	if warning := judgeWarning(judgeScore{Score: 4, Pass: true}, 4); warning != "" {
		t.Fatalf("unexpected warning: %s", warning)
	}
	if warning := judgeWarning(judgeScore{Score: 2, Pass: true}, 4); warning == "" {
		t.Fatal("expected warning for conflicting model pass")
	}
}

func TestSummarizeJudgeResults(t *testing.T) {
	results := []CaseResult{
		{JudgeEvaluated: true, JudgePass: true, JudgeScore: 5, JudgeFaithfulnessScore: 4, JudgeAnswerRelevanceScore: 3, JudgeContextPrecisionScore: 2, JudgeLatencyMS: 20},
		{JudgeEvaluated: true, JudgePass: false, JudgeScore: 3, JudgeFaithfulnessScore: 2, JudgeAnswerRelevanceScore: 3, JudgeContextPrecisionScore: 4, JudgeLatencyMS: 40},
		{JudgeError: "failed"},
	}
	got := summarize(results, make([]EvalCaseRequest, len(results)))
	if got.JudgeEvaluated != 2 || got.JudgePassRate != 0.5 {
		t.Fatalf("unexpected judge summary: %+v", got)
	}
	if got.AvgJudgeScore != 4 || got.AvgJudgeFaithfulnessScore != 3 || got.AvgJudgeAnswerRelevanceScore != 3 || got.AvgJudgeContextPrecisionScore != 3 || got.AvgJudgeLatencyMS != 30 {
		t.Fatalf("unexpected judge averages: %+v", got)
	}
}

func TestJudgeEnabledDefaultsToTrue(t *testing.T) {
	req := EvalRequest{}
	if !(req.JudgeEnabled == nil || *req.JudgeEnabled) {
		t.Fatal("expected omitted judge_enabled to mean enabled")
	}
	disabled := false
	req.JudgeEnabled = &disabled
	if req.JudgeEnabled == nil || *req.JudgeEnabled {
		t.Fatal("expected explicit false to disable judge")
	}
}

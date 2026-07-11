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
	got, err := parseJudgeScore("```json\n{\"correctness\":0.9,\"faithfulness\":0.8,\"relevance\":0.7,\"reason\":\"supported\"}\n```")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Correctness != 0.9 || got.Faithfulness != 0.8 || got.Relevance != 0.7 || got.Reason != "supported" {
		t.Fatalf("unexpected score: %+v", got)
	}
}

func TestParseJudgeScoreRejectsInvalidResponses(t *testing.T) {
	tests := []string{
		`not json`,
		`{"correctness":1.1,"faithfulness":0.8,"relevance":0.7,"reason":"bad"}`,
		`{"correctness":0.8,"faithfulness":0.8,"reason":"missing relevance"}`,
		`{"correctness":0.8,"faithfulness":0.8,"relevance":0.8,"reason":""}`,
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

func TestJudgePassedRequiresEveryDimensionAtThreshold(t *testing.T) {
	if !judgePassed(judgeScore{Correctness: 0.7, Faithfulness: 0.8, Relevance: 0.9}, 0.7) {
		t.Fatal("expected score at threshold to pass")
	}
	if judgePassed(judgeScore{Correctness: 0.69, Faithfulness: 1, Relevance: 1}, 0.7) {
		t.Fatal("expected one dimension below threshold to fail")
	}
}

func TestSummarizeJudgeResults(t *testing.T) {
	results := []CaseResult{
		{JudgeEvaluated: true, JudgePass: true, JudgeCorrectness: 0.9, JudgeFaithfulness: 0.8, JudgeRelevance: 0.7, JudgeLatencyMS: 20},
		{JudgeEvaluated: true, JudgePass: false, JudgeCorrectness: 0.5, JudgeFaithfulness: 0.6, JudgeRelevance: 0.7, JudgeLatencyMS: 40},
		{JudgeError: "failed"},
	}
	got := summarize(results, make([]EvalCaseRequest, len(results)))
	if got.JudgeEvaluated != 2 || got.JudgePassRate != 0.5 {
		t.Fatalf("unexpected judge summary: %+v", got)
	}
	if got.AvgJudgeCorrectness != 0.7 || got.AvgJudgeFaithfulness != 0.7 || got.AvgJudgeRelevance != 0.7 || got.AvgJudgeLatencyMS != 30 {
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

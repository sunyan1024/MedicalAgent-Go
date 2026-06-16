package rageval

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestCheckAnswerPassesWhenKeywordsMatchAndForbiddenAbsent(t *testing.T) {
	got := checkAnswer("2023年毕业人数为1200人。", []string{"2023", "毕业人数", "1200"}, []string{"无法找到"})

	if !got.Pass {
		t.Fatalf("expected answer to pass, got missing=%v forbidden=%v validation=%v", got.MissingKeywords, got.ForbiddenHits, got.ValidationErrors)
	}
}

func TestCheckAnswerFailsWhenKeywordMissing(t *testing.T) {
	got := checkAnswer("2023年毕业人数为1200人。", []string{"2024"}, nil)

	if got.Pass {
		t.Fatal("expected answer to fail")
	}
	if len(got.MissingKeywords) != 1 || got.MissingKeywords[0] != "2024" {
		t.Fatalf("unexpected missing keywords: %v", got.MissingKeywords)
	}
}

func TestCheckAnswerFailsWhenForbiddenKeywordAppears(t *testing.T) {
	got := checkAnswer("无法找到相关信息。", nil, []string{"无法找到"})

	if got.Pass {
		t.Fatal("expected answer to fail")
	}
	if len(got.ForbiddenHits) != 1 || got.ForbiddenHits[0] != "无法找到" {
		t.Fatalf("unexpected forbidden hits: %v", got.ForbiddenHits)
	}
}

func TestCheckAnswerRequiresAtLeastOneRule(t *testing.T) {
	got := checkAnswer("任意回答", nil, nil)

	if got.Pass {
		t.Fatal("expected answer to fail without rules")
	}
	if len(got.ValidationErrors) != 1 {
		t.Fatalf("expected one validation error, got %v", got.ValidationErrors)
	}
}

func TestRetrievedSourcesUsesOriginalNameThenFilenameThenMetadata(t *testing.T) {
	docs := []*schema.Document{
		{MetaData: map[string]any{"original_name": "原始文件.pdf", "filename": "stored.pdf"}},
		{MetaData: map[string]any{"filename": "stored.txt"}},
		{MetaData: map[string]any{"metadata": "uploads/stored/data.md"}},
		nil,
	}

	got := retrievedSources(docs)
	want := []string{"原始文件.pdf", "stored.txt", "data.md", "文档 4"}

	if len(got) != len(want) {
		t.Fatalf("expected %d sources, got %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("source[%d] expected %q, got %q", i, want[i], got[i])
		}
	}
}

func TestScoreRetrievalMatchesCaseInsensitively(t *testing.T) {
	hit, rank, score := scoreRetrieval([]string{"other.md", "Annual-Report.PDF"}, []string{"annual-report.pdf"})

	if !hit || rank != 2 || score != 0.5 {
		t.Fatalf("expected hit at rank 2 with score 0.5, got hit=%v rank=%d score=%f", hit, rank, score)
	}
}

func TestSummarizeSkipsRetrievalCasesWithoutExpectedSources(t *testing.T) {
	results := []CaseResult{
		{
			RetrievalHit:        true,
			RetrievalScore:      1,
			AnswerPass:          true,
			RetrievedDocCount:   5,
			RetrievalLatencyMS:  10,
			GenerationLatencyMS: 20,
			LatencyMS:           30,
		},
		{
			AnswerPass:          false,
			RetrievedDocCount:   3,
			RetrievalLatencyMS:  20,
			GenerationLatencyMS: 40,
			LatencyMS:           60,
			Error:               "generate failed",
		},
	}
	cases := []EvalCaseRequest{
		{ExpectedSources: []string{"a.pdf"}},
		{},
	}

	got := summarize(results, cases)
	if got.Total != 2 {
		t.Fatalf("expected total 2, got %d", got.Total)
	}
	if got.RetrievalEvaluated != 1 || got.RetrievalRecallAt5 != 1 || got.MRR != 1 {
		t.Fatalf("unexpected retrieval summary: %+v", got)
	}
	if got.AnswerPassRate != 0.5 || got.ErrorRate != 0.5 {
		t.Fatalf("unexpected pass/error rates: %+v", got)
	}
	if got.AvgRetrievedDocs != 4 || got.AvgRetrievalLatencyMS != 15 || got.AvgGenerationLatencyMS != 30 || got.AvgLatencyMS != 45 {
		t.Fatalf("unexpected averages: %+v", got)
	}
}

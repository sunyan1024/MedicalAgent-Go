package rageval

import (
	"GoAI/common/rag"
	"GoAI/config"
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/qwen"
	"github.com/cloudwego/eino/schema"
)

const MaxEvalCases = 50

type EvalRequest struct {
	Cases        []EvalCaseRequest `json:"cases" binding:"required"`
	JudgeEnabled *bool             `json:"judge_enabled,omitempty"`
}

type EvalCaseRequest struct {
	ID                string   `json:"id"`
	Question          string   `json:"question" binding:"required"`
	ExpectedAnswer    string   `json:"expected_answer"`
	ExpectedKeywords  []string `json:"expected_keywords"`
	ForbiddenKeywords []string `json:"forbidden_keywords"`
	ExpectedSources   []string `json:"expected_sources"`
}

type Summary struct {
	Total                  int     `json:"total"`
	RetrievalEvaluated     int     `json:"retrieval_evaluated"`
	RetrievalRecallAt5     float64 `json:"retrieval_recall_at_5"`
	MRR                    float64 `json:"mrr"`
	AnswerPassRate         float64 `json:"answer_pass_rate"`
	ErrorRate              float64 `json:"error_rate"`
	AvgRetrievedDocs       float64 `json:"avg_retrieved_docs"`
	AvgRetrievalLatencyMS  int64   `json:"avg_retrieval_latency_ms"`
	AvgGenerationLatencyMS int64   `json:"avg_generation_latency_ms"`
	JudgeEvaluated         int     `json:"judge_evaluated"`
	JudgePassRate          float64 `json:"judge_pass_rate"`
	AvgJudgeCorrectness    float64 `json:"avg_judge_correctness"`
	AvgJudgeFaithfulness   float64 `json:"avg_judge_faithfulness"`
	AvgJudgeRelevance      float64 `json:"avg_judge_relevance"`
	AvgJudgeLatencyMS      int64   `json:"avg_judge_latency_ms"`
	AvgLatencyMS           int64   `json:"avg_latency_ms"`
}

type CaseResult struct {
	ID                  string   `json:"id"`
	Question            string   `json:"question"`
	ExpectedAnswer      string   `json:"expected_answer,omitempty"`
	RetrievedSources    []string `json:"retrieved_sources"`
	RetrievedDocCount   int      `json:"retrieved_doc_count"`
	RetrievalHit        bool     `json:"retrieval_hit"`
	RetrievalRank       int      `json:"retrieval_rank,omitempty"`
	RetrievalScore      float64  `json:"retrieval_score"`
	AnswerPass          bool     `json:"answer_pass"`
	MissingKeywords     []string `json:"missing_keywords,omitempty"`
	ForbiddenHits       []string `json:"forbidden_hits,omitempty"`
	ValidationErrors    []string `json:"validation_errors,omitempty"`
	Answer              string   `json:"answer"`
	RetrievalLatencyMS  int64    `json:"retrieval_latency_ms"`
	GenerationLatencyMS int64    `json:"generation_latency_ms"`
	JudgeEvaluated      bool     `json:"judge_evaluated"`
	JudgePass           bool     `json:"judge_pass"`
	JudgeCorrectness    float64  `json:"judge_correctness"`
	JudgeFaithfulness   float64  `json:"judge_faithfulness"`
	JudgeRelevance      float64  `json:"judge_relevance"`
	JudgeReason         string   `json:"judge_reason,omitempty"`
	JudgeLatencyMS      int64    `json:"judge_latency_ms"`
	JudgeError          string   `json:"judge_error,omitempty"`
	LatencyMS           int64    `json:"latency_ms"`
	Error               string   `json:"error,omitempty"`
}

type EvalResponse struct {
	Summary Summary      `json:"summary"`
	Cases   []CaseResult `json:"cases"`
}

type answerCheck struct {
	Pass             bool
	MissingKeywords  []string
	ForbiddenHits    []string
	ValidationErrors []string
}

func Evaluate(ctx context.Context, username string, req EvalRequest) (*EvalResponse, error) {
	if err := validateRequest(req); err != nil {
		return nil, err
	}

	query, queryErr := rag.NewRAGQuery(ctx, username)
	chatModel, chatErr := newRAGChatModel(ctx)
	judgeEnabled := req.JudgeEnabled == nil || *req.JudgeEnabled
	var judge judgeModel
	var judgeErr error
	if judgeEnabled {
		judge, judgeErr = newJudgeModel(ctx)
	}

	results := make([]CaseResult, 0, len(req.Cases))
	for _, c := range req.Cases {
		caseStart := time.Now()
		result := CaseResult{
			ID:             strings.TrimSpace(c.ID),
			Question:       c.Question,
			ExpectedAnswer: c.ExpectedAnswer,
		}

		if queryErr != nil {
			result.Error = fmt.Sprintf("query init failed: %v", queryErr)
			result.LatencyMS = elapsedMS(caseStart)
			results = append(results, result)
			continue
		}

		retrieveStart := time.Now()
		docs, err := query.RetrieveDocuments(ctx, c.Question)
		result.RetrievalLatencyMS = elapsedMS(retrieveStart)
		if err != nil {
			result.Error = fmt.Sprintf("retrieve failed: %v", err)
			result.LatencyMS = elapsedMS(caseStart)
			results = append(results, result)
			continue
		}
		result.RetrievedDocCount = len(docs)
		result.RetrievedSources = retrievedSources(docs)
		result.RetrievalHit, result.RetrievalRank, result.RetrievalScore = scoreRetrieval(result.RetrievedSources, c.ExpectedSources)

		if chatErr != nil {
			result.Error = fmt.Sprintf("chat model init failed: %v", chatErr)
			result.LatencyMS = elapsedMS(caseStart)
			results = append(results, result)
			continue
		}

		prompt := rag.BuildRAGPrompt(c.Question, docs)
		generationStart := time.Now()
		resp, err := chatModel.Generate(ctx, []*schema.Message{
			{Role: schema.User, Content: prompt},
		})
		result.GenerationLatencyMS = elapsedMS(generationStart)
		if err != nil {
			result.Error = fmt.Sprintf("generate failed: %v", err)
			result.LatencyMS = elapsedMS(caseStart)
			results = append(results, result)
			continue
		}
		if resp != nil {
			result.Answer = resp.Content
		}

		check := checkAnswer(result.Answer, c.ExpectedKeywords, c.ForbiddenKeywords)
		result.AnswerPass = check.Pass
		result.MissingKeywords = check.MissingKeywords
		result.ForbiddenHits = check.ForbiddenHits
		result.ValidationErrors = check.ValidationErrors

		if judgeEnabled && strings.TrimSpace(c.ExpectedAnswer) != "" {
			judgeStart := time.Now()
			if judgeErr != nil {
				result.JudgeError = fmt.Sprintf("judge model init failed: %v", judgeErr)
			} else {
				score, err := runJudge(ctx, judge, c, result.Answer, docs)
				if err != nil {
					result.JudgeError = err.Error()
				} else {
					result.JudgeEvaluated = true
					result.JudgeCorrectness = score.Correctness
					result.JudgeFaithfulness = score.Faithfulness
					result.JudgeRelevance = score.Relevance
					result.JudgeReason = score.Reason
					threshold := judgeThreshold()
					result.JudgePass = judgePassed(score, threshold)
				}
			}
			result.JudgeLatencyMS = elapsedMS(judgeStart)
		}
		result.LatencyMS = elapsedMS(caseStart)
		results = append(results, result)
	}

	return &EvalResponse{
		Summary: summarize(results, req.Cases),
		Cases:   results,
	}, nil
}

func validateRequest(req EvalRequest) error {
	if len(req.Cases) == 0 {
		return fmt.Errorf("cases cannot be empty")
	}
	if len(req.Cases) > MaxEvalCases {
		return fmt.Errorf("cases cannot exceed %d", MaxEvalCases)
	}
	for i, c := range req.Cases {
		if strings.TrimSpace(c.Question) == "" {
			return fmt.Errorf("cases[%d].question cannot be empty", i)
		}
	}
	return nil
}

func newRAGChatModel(ctx context.Context) (*qwen.ChatModel, error) {
	conf := config.GetConfig()
	enableThinking := conf.RagModelConfig.EnableThinking
	chatConfig := &qwen.ChatModelConfig{
		BaseURL:        conf.RagModelConfig.RagBaseUrl,
		Model:          conf.RagModelConfig.RagChatModelName,
		APIKey:         conf.RagModelConfig.RagApiKey,
		EnableThinking: &enableThinking,
	}
	chatModel, err := qwen.NewChatModel(ctx, chatConfig)
	if err != nil {
		return nil, fmt.Errorf("create rag chat model failed: %w", err)
	}
	return chatModel, nil
}

func retrievedSources(docs []*schema.Document) []string {
	sources := make([]string, 0, len(docs))
	for i, doc := range docs {
		source := sourceLabel(doc)
		if source == "" {
			source = "文档 " + strconv.Itoa(i+1)
		}
		sources = append(sources, source)
	}
	return sources
}

func sourceLabel(doc *schema.Document) string {
	if doc == nil || doc.MetaData == nil {
		return ""
	}

	for _, key := range []string{"original_name", "filename", "metadata"} {
		if val, ok := doc.MetaData[key].(string); ok {
			val = strings.TrimSpace(val)
			if val == "" {
				continue
			}
			if key == "metadata" {
				if base := filepath.Base(val); base != "." && base != string(filepath.Separator) {
					return base
				}
			}
			return val
		}
	}
	return ""
}

func scoreRetrieval(retrievedSources []string, expectedSources []string) (bool, int, float64) {
	if len(expectedSources) == 0 {
		return false, 0, 0
	}

	for i, source := range retrievedSources {
		if sourceMatches(source, expectedSources) {
			rank := i + 1
			return true, rank, 1 / float64(rank)
		}
	}
	return false, 0, 0
}

func sourceMatches(source string, expectedSources []string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	if source == "" {
		return false
	}
	for _, expected := range expectedSources {
		expected = strings.ToLower(strings.TrimSpace(expected))
		if expected == "" {
			continue
		}
		if strings.Contains(source, expected) || strings.Contains(expected, source) {
			return true
		}
	}
	return false
}

func checkAnswer(answer string, expectedKeywords []string, forbiddenKeywords []string) answerCheck {
	answerLower := strings.ToLower(answer)
	check := answerCheck{Pass: true}

	if len(expectedKeywords) == 0 && len(forbiddenKeywords) == 0 {
		check.Pass = false
		check.ValidationErrors = append(check.ValidationErrors, "expected_keywords or forbidden_keywords must be provided")
		return check
	}

	for _, keyword := range expectedKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if !strings.Contains(answerLower, strings.ToLower(keyword)) {
			check.MissingKeywords = append(check.MissingKeywords, keyword)
		}
	}

	for _, keyword := range forbiddenKeywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}
		if strings.Contains(answerLower, strings.ToLower(keyword)) {
			check.ForbiddenHits = append(check.ForbiddenHits, keyword)
		}
	}

	if len(check.MissingKeywords) > 0 || len(check.ForbiddenHits) > 0 {
		check.Pass = false
	}
	return check
}

func summarize(results []CaseResult, cases []EvalCaseRequest) Summary {
	summary := Summary{Total: len(results)}
	if len(results) == 0 {
		return summary
	}

	var retrievalHits int
	var answerPasses int
	var errors int
	var totalRetrievedDocs int
	var totalRetrievalLatency int64
	var totalGenerationLatency int64
	var judgePasses int
	var totalJudgeCorrectness float64
	var totalJudgeFaithfulness float64
	var totalJudgeRelevance float64
	var totalJudgeLatency int64
	var totalLatency int64
	var mrrSum float64

	for i, result := range results {
		if i < len(cases) && len(cases[i].ExpectedSources) > 0 {
			summary.RetrievalEvaluated++
			if result.RetrievalHit {
				retrievalHits++
			}
			mrrSum += result.RetrievalScore
		}
		if result.AnswerPass {
			answerPasses++
		}
		if result.Error != "" {
			errors++
		}
		totalRetrievedDocs += result.RetrievedDocCount
		totalRetrievalLatency += result.RetrievalLatencyMS
		totalGenerationLatency += result.GenerationLatencyMS
		if result.JudgeEvaluated {
			summary.JudgeEvaluated++
			if result.JudgePass {
				judgePasses++
			}
			totalJudgeCorrectness += result.JudgeCorrectness
			totalJudgeFaithfulness += result.JudgeFaithfulness
			totalJudgeRelevance += result.JudgeRelevance
			totalJudgeLatency += result.JudgeLatencyMS
		}
		totalLatency += result.LatencyMS
	}

	if summary.RetrievalEvaluated > 0 {
		summary.RetrievalRecallAt5 = float64(retrievalHits) / float64(summary.RetrievalEvaluated)
		summary.MRR = mrrSum / float64(summary.RetrievalEvaluated)
	}
	summary.AnswerPassRate = float64(answerPasses) / float64(len(results))
	summary.ErrorRate = float64(errors) / float64(len(results))
	summary.AvgRetrievedDocs = float64(totalRetrievedDocs) / float64(len(results))
	summary.AvgRetrievalLatencyMS = totalRetrievalLatency / int64(len(results))
	summary.AvgGenerationLatencyMS = totalGenerationLatency / int64(len(results))
	if summary.JudgeEvaluated > 0 {
		count := float64(summary.JudgeEvaluated)
		summary.JudgePassRate = float64(judgePasses) / count
		summary.AvgJudgeCorrectness = totalJudgeCorrectness / count
		summary.AvgJudgeFaithfulness = totalJudgeFaithfulness / count
		summary.AvgJudgeRelevance = totalJudgeRelevance / count
		summary.AvgJudgeLatencyMS = totalJudgeLatency / int64(summary.JudgeEvaluated)
	}
	summary.AvgLatencyMS = totalLatency / int64(len(results))
	return summary
}

func elapsedMS(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

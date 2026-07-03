package rag

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode"

	"GoAI/config"
	paddleocr "github.com/PaddlePaddle/PaddleOCR/api_sdk/go"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/schema"
	pdftotext "github.com/heussd/pdftotext-go"
)

type stubPaddleDocParser struct {
	result    *paddleocr.DocParsingResult
	err       error
	req       *paddleocr.DocParsingRequest
	job       *paddleocr.Job
	submitErr error
	status    *paddleocr.JobStatus
	statusErr error
}

type stubRetriever struct {
	docs []*schema.Document
	err  error
}

func (s stubRetriever) Retrieve(ctx context.Context, query string, opts ...retriever.Option) ([]*schema.Document, error) {
	return s.docs, s.err
}

type stubReranker struct {
	docs []*schema.Document
	err  error
}

func (s stubReranker) Rerank(ctx context.Context, query string, docs []*schema.Document, topK int) ([]*schema.Document, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.docs, nil
}

func (s *stubPaddleDocParser) ParseDocument(ctx context.Context, req *paddleocr.DocParsingRequest) (*paddleocr.DocParsingResult, error) {
	s.req = req
	return s.result, s.err
}

func (s *stubPaddleDocParser) SubmitDocumentParsing(ctx context.Context, req *paddleocr.DocParsingRequest) (*paddleocr.Job, error) {
	s.req = req
	if s.submitErr != nil {
		return nil, s.submitErr
	}
	if s.job != nil {
		return s.job, nil
	}
	return &paddleocr.Job{JobID: "job-test"}, nil
}

func (s *stubPaddleDocParser) GetStatus(ctx context.Context, jobID string) (*paddleocr.JobStatus, error) {
	if s.statusErr != nil {
		return nil, s.statusErr
	}
	if s.status != nil {
		return s.status, nil
	}
	return nil, fmt.Errorf("GetStatus is not stubbed")
}

func withOCRConfig(t *testing.T, token, model string, timeoutSeconds int) {
	t.Helper()
	restoreWorkingDir := chdirProjectRoot(t)
	defer restoreWorkingDir()

	conf := config.GetConfig()
	originalToken := conf.RagModelConfig.OCRAccessToken
	originalModel := conf.RagModelConfig.OCRModel
	originalTimeout := conf.RagModelConfig.OCRTimeoutSeconds
	conf.RagModelConfig.OCRAccessToken = token
	conf.RagModelConfig.OCRModel = model
	conf.RagModelConfig.OCRTimeoutSeconds = timeoutSeconds

	t.Cleanup(func() {
		conf.RagModelConfig.OCRAccessToken = originalToken
		conf.RagModelConfig.OCRModel = originalModel
		conf.RagModelConfig.OCRTimeoutSeconds = originalTimeout
	})
}

func chdirProjectRoot(t *testing.T) func() {
	t.Helper()

	originalDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	dir := originalDir
	for {
		if _, err := os.Stat(filepath.Join(dir, "config", "config.toml")); err == nil {
			if err := os.Chdir(dir); err != nil {
				t.Fatalf("change to project root: %v", err)
			}
			return func() {
				if err := os.Chdir(originalDir); err != nil {
					t.Fatalf("restore working directory: %v", err)
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("project root with config/config.toml not found from %s", originalDir)
		}
		dir = parent
	}
}

func withPaddleDocParser(t *testing.T, parser *stubPaddleDocParser) {
	t.Helper()

	original := newPaddleDocParser
	newPaddleDocParser = func(token string, timeout time.Duration) (paddleDocumentParser, error) {
		if token != "test-token" {
			t.Fatalf("unexpected token: %q", token)
		}
		if timeout != 2*time.Second {
			t.Fatalf("unexpected timeout: %v", timeout)
		}
		return parser, nil
	}
	t.Cleanup(func() {
		newPaddleDocParser = original
	})
}

func TestBuildRAGPromptUsesOriginalName(t *testing.T) {
	prompt := BuildRAGPrompt("2023毕业人数是多少", []*schema.Document{
		{
			Content: "毕业生总人数为 1234 人。",
			MetaData: map[string]any{
				"filename":      "47732580096_abc.pdf",
				"original_name": "2023届就业质量报告.pdf",
			},
		},
	})

	if !strings.Contains(prompt, "[2023届就业质量报告.pdf]: 毕业生总人数为 1234 人。") {
		t.Fatalf("prompt does not use original file name: %s", prompt)
	}
	if strings.Contains(prompt, "[文档 1]") {
		t.Fatalf("prompt should not use generic document label when original name exists: %s", prompt)
	}
}

func TestBuildRAGPromptFallsBackAndSkipsNilDocs(t *testing.T) {
	prompt := BuildRAGPrompt("问题", []*schema.Document{
		nil,
		{
			Content: "stored name content",
			MetaData: map[string]any{
				"filename": "stored.pdf",
			},
		},
		{
			Content:  "fallback content",
			MetaData: map[string]any{},
		},
	})

	if !strings.Contains(prompt, "[stored.pdf]: stored name content") {
		t.Fatalf("prompt does not fall back to stored filename: %s", prompt)
	}
	if !strings.Contains(prompt, "[文档 3]: fallback content") {
		t.Fatalf("prompt does not fall back to generic label: %s", prompt)
	}
}

func TestBuildRAGPromptReturnsQueryForEmptyContext(t *testing.T) {
	query := "问题"

	if got := BuildRAGPrompt(query, nil); got != query {
		t.Fatalf("empty docs: got %q, want %q", got, query)
	}
	if got := BuildRAGPrompt(query, []*schema.Document{nil}); got != query {
		t.Fatalf("nil-only docs: got %q, want %q", got, query)
	}
}

func TestApplyRerankResultsSortsByScoreAndPreservesUnrankedDocs(t *testing.T) {
	docs := []*schema.Document{
		{ID: "a", Content: "alpha", MetaData: map[string]any{"distance": 0.1}},
		{ID: "b", Content: "beta", MetaData: map[string]any{"distance": 0.2}},
		{ID: "c", Content: "gamma", MetaData: map[string]any{"distance": 0.3}},
	}

	got := applyRerankResults(docs, []dashScopeRerankResult{
		{Index: 1, RelevanceScore: 0.91},
		{Index: 0, RelevanceScore: 0.44},
	})

	if len(got) != 3 {
		t.Fatalf("reranked docs length: got %d", len(got))
	}
	if got[0].ID != "b" || got[1].ID != "a" || got[2].ID != "c" {
		t.Fatalf("unexpected rerank order: %s, %s, %s", got[0].ID, got[1].ID, got[2].ID)
	}
	if score, ok := rerankScore(got[0]); !ok || score != 0.91 {
		t.Fatalf("first doc rerank score: got score=%f ok=%v", score, ok)
	}
	if _, ok := rerankScore(got[2]); ok {
		t.Fatal("unranked doc should not have rerank score")
	}
}

func TestApplyRerankResultsIgnoresInvalidAndDuplicateIndexes(t *testing.T) {
	docs := []*schema.Document{
		{ID: "a", Content: "alpha"},
		{ID: "b", Content: "beta"},
	}

	got := applyRerankResults(docs, []dashScopeRerankResult{
		{Index: 5, RelevanceScore: 0.99},
		{Index: 1, RelevanceScore: 0.80},
		{Index: 1, RelevanceScore: 0.70},
		{Index: -1, RelevanceScore: 0.60},
	})

	if len(got) != 2 {
		t.Fatalf("reranked docs length: got %d", len(got))
	}
	if got[0].ID != "b" || got[1].ID != "a" {
		t.Fatalf("unexpected rerank order: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestNewRetrieveOptionsDefaultsAndNormalizesTopK(t *testing.T) {
	opts := newRetrieveOptions(config.RagModelConfig{
		RerankEnabled:       true,
		RerankCandidateTopK: 2,
		RerankTopK:          5,
	})

	if !opts.rerankEnabled {
		t.Fatal("rerank should be enabled")
	}
	if opts.rerankModel != defaultRerankModel {
		t.Fatalf("rerank model: got %q", opts.rerankModel)
	}
	if opts.rerankBaseURL != defaultRerankBaseURL {
		t.Fatalf("rerank base url: got %q", opts.rerankBaseURL)
	}
	if opts.rerankCandidateTopK != 5 || opts.rerankTopK != 5 {
		t.Fatalf("topK should normalize candidate>=final, got candidate=%d final=%d", opts.rerankCandidateTopK, opts.rerankTopK)
	}
	if opts.rerankTimeout != defaultRerankTimeoutSeconds*time.Second {
		t.Fatalf("timeout: got %v", opts.rerankTimeout)
	}
}

func TestRetrieveDocumentsFallsBackToDistanceSortWhenRerankFails(t *testing.T) {
	query := &RAGQuery{
		retrievers: []retriever.Retriever{
			stubRetriever{docs: []*schema.Document{
				{ID: "far", Content: "far", MetaData: map[string]any{"distance": "0.8"}},
				{ID: "near", Content: "near", MetaData: map[string]any{"distance": "0.1"}},
				{ID: "middle", Content: "middle", MetaData: map[string]any{"distance": "0.4"}},
			}},
		},
		reranker:  stubReranker{err: fmt.Errorf("rerank unavailable")},
		finalTopK: 2,
	}

	got, err := query.RetrieveDocuments(context.Background(), "question")
	if err != nil {
		t.Fatalf("retrieve documents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("docs length: got %d", len(got))
	}
	if got[0].ID != "near" || got[1].ID != "middle" {
		t.Fatalf("unexpected fallback order: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestRetrieveDocumentsUsesRerankOrder(t *testing.T) {
	docs := []*schema.Document{
		{ID: "near", Content: "near", MetaData: map[string]any{"distance": 0.1}},
		{ID: "middle", Content: "middle", MetaData: map[string]any{"distance": 0.4}},
		{ID: "far", Content: "far", MetaData: map[string]any{"distance": 0.8}},
	}
	query := &RAGQuery{
		retrievers: []retriever.Retriever{
			stubRetriever{docs: docs},
		},
		reranker: stubReranker{docs: []*schema.Document{
			docs[2],
			docs[0],
			docs[1],
		}},
		finalTopK: 2,
	}

	got, err := query.RetrieveDocuments(context.Background(), "question")
	if err != nil {
		t.Fatalf("retrieve documents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("docs length: got %d", len(got))
	}
	if got[0].ID != "far" || got[1].ID != "near" {
		t.Fatalf("unexpected rerank order: %s, %s", got[0].ID, got[1].ID)
	}
}

func TestBuildFileIndexKey(t *testing.T) {
	if got := BuildFileIndexKey("user1", "abc.pdf"); got != "user1:abc.pdf" {
		t.Fatalf("scoped key: got %q", got)
	}
	if got := BuildFileIndexKey("", "fallback.pdf"); got != "fallback.pdf" {
		t.Fatalf("empty username key: got %q", got)
	}
}

func TestBuildStoredFileIndexKey(t *testing.T) {
	if got := BuildStoredFileIndexKey("abc"); got != "stored:abc" {
		t.Fatalf("stored file key: got %q", got)
	}
}

func TestJoinPDFPageTextSortsPagesAndSkipsEmptyContent(t *testing.T) {
	pages := []pdftotext.PdfPage{
		{Number: 2, Content: " 第二页 "},
		{Number: 1, Content: "第一页"},
		{Number: 3, Content: " \n\t "},
	}

	if got := joinPDFPageText(pages); got != "第一页\n第二页" {
		t.Fatalf("joined pages: got %q", got)
	}
}

func TestJoinPDFPageTextReturnsEmptyForBlankPages(t *testing.T) {
	pages := []pdftotext.PdfPage{
		{Number: 1, Content: " \n\t "},
		{Number: 2, Content: ""},
	}

	if got := joinPDFPageText(pages); got != "" {
		t.Fatalf("blank pages should produce empty text, got %q", got)
	}
}

func TestValidateExtractedPDFTextAcceptsReadableText(t *testing.T) {
	text := "OpenClaw 橙皮书主要内容\nPart 1: 初识 OpenClaw\tPart 5: Skills 系统。"

	if err := validateExtractedPDFText(text); err != nil {
		t.Fatalf("readable text should pass: %v", err)
	}
}

func TestValidateExtractedPDFTextRejectsGarbledText(t *testing.T) {
	text := "OpenClaw" + strings.Repeat("\u008c", 3)

	if err := validateExtractedPDFText(text); err == nil {
		t.Fatal("garbled text should fail")
	} else if !strings.Contains(err.Error(), "garbled content ratio too high") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtractedPDFTextRejectsEmptyText(t *testing.T) {
	if err := validateExtractedPDFText(" \n\t "); err == nil {
		t.Fatal("empty text should fail")
	} else if !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtractedPDFTextThresholdBoundary(t *testing.T) {
	atThreshold := strings.Repeat("a", 4) + "\u008c"
	overThreshold := strings.Repeat("a", 3) + "\u008c"

	if err := validateExtractedPDFText(atThreshold); err != nil {
		t.Fatalf("20%% garbled content should pass: %v", err)
	}
	if err := validateExtractedPDFText(overThreshold); err == nil {
		t.Fatal("content over 20% garbled should fail")
	}
}

func TestExtractPDFTextWithFallbackUsesOCRFirst(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		t.Fatalf("native extractor should not be called when OCR succeeds, file=%s", filePath)
		return "", nil
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "ocr-first.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return " OCR 图片中文字 ", nil
	}

	got, err := extractPDFTextWithFallback("ocr-first.pdf")
	if err != nil {
		t.Fatalf("fallback should return OCR text: %v", err)
	}
	if got != "OCR 图片中文字" {
		t.Fatalf("got %q, want OCR text", got)
	}
}

func TestExtractPDFTextWithFallbackUsesNativeWhenOCRFails(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "native-fallback.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return "native text", nil
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "native-fallback.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return "", fmt.Errorf("ocr unavailable")
	}

	got, err := extractPDFTextWithFallback("native-fallback.pdf")
	if err != nil {
		t.Fatalf("fallback should use native text without error: %v", err)
	}
	if got != "native text" {
		t.Fatalf("got %q, want native text", got)
	}
}

func TestExtractPDFTextWithFallbackUsesNativeWhenOCRTextIsEmpty(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "empty-ocr.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return "native text after empty ocr", nil
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "empty-ocr.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return " \n\t ", nil
	}

	got, err := extractPDFTextWithFallback("empty-ocr.pdf")
	if err != nil {
		t.Fatalf("fallback should use native text after empty OCR: %v", err)
	}
	if got != "native text after empty ocr" {
		t.Fatalf("got %q, want native text", got)
	}
}

func TestExtractPDFTextWithFallbackUsesNativeWhenOCRTextIsGarbled(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "garbled-ocr.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return "native text after garbled ocr", nil
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		if filePath != "garbled-ocr.pdf" {
			t.Fatalf("unexpected file path: %s", filePath)
		}
		return "abc" + strings.Repeat("\u008c", 2), nil
	}

	got, err := extractPDFTextWithFallback("garbled-ocr.pdf")
	if err != nil {
		t.Fatalf("fallback should use native text after garbled OCR: %v", err)
	}
	if got != "native text after garbled ocr" {
		t.Fatalf("got %q, want native text", got)
	}
}

func TestExtractPDFTextWithFallbackReturnsCombinedErrorWhenBothFail(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		return "", fmt.Errorf("native failed")
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		return "", fmt.Errorf("ocr failed")
	}

	_, err := extractPDFTextWithFallback("bad.pdf")
	if err == nil {
		t.Fatal("expected combined error")
	}
	if !strings.Contains(err.Error(), "ocr failed") || !strings.Contains(err.Error(), "native failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractPDFTextWithOCRRequiresAccessToken(t *testing.T) {
	withOCRConfig(t, "", paddleocr.PaddleOCRVL16, 2)

	_, err := extractPDFTextWithOCR("scan.pdf")
	if err == nil {
		t.Fatal("expected missing token error")
	}
	if !strings.Contains(err.Error(), "ocrAccessToken") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractPDFTextWithOCRRejectsNonDocumentParsingModel(t *testing.T) {
	withOCRConfig(t, "test-token", paddleocr.PPOCRv6, 2)

	_, err := extractPDFTextWithOCR("scan.pdf")
	if err == nil {
		t.Fatal("expected invalid model error")
	}
	if !strings.Contains(err.Error(), "not a document parsing model") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractPDFTextWithOCRUsesParseDocumentMarkdownText(t *testing.T) {
	withOCRConfig(t, "test-token", paddleocr.PaddleOCRVL16, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"result":{"layoutParsingResults":[{"markdown":{"text":"# 第一页\n正文"}},{"markdown":{"text":"## 第二页\n表格内容"}}]}}`)
	}))
	defer server.Close()

	parser := &stubPaddleDocParser{
		job: &paddleocr.Job{JobID: "job-doc"},
		status: &paddleocr.JobStatus{
			JobID:     "job-doc",
			State:     "done",
			ResultURL: map[string]string{"jsonUrl": server.URL},
		},
	}
	withPaddleDocParser(t, parser)

	got, err := extractPDFTextWithOCR("scan.pdf")
	if err != nil {
		t.Fatalf("extractPDFTextWithOCR returned error: %v", err)
	}
	if got != "# 第一页\n正文\n## 第二页\n表格内容" {
		t.Fatalf("got %q", got)
	}
	if parser.req == nil {
		t.Fatal("SubmitDocumentParsing was not called")
	}
	if parser.req.Model != paddleocr.PaddleOCRVL16 {
		t.Fatalf("unexpected model: %s", parser.req.Model)
	}
	if parser.req.FilePath != "scan.pdf" {
		t.Fatalf("unexpected file path: %s", parser.req.FilePath)
	}
	if _, ok := parser.req.Options.(*paddleocr.PaddleOCRVLOptions); !ok {
		t.Fatalf("expected PaddleOCR VL options, got %T", parser.req.Options)
	}
}

func TestExtractDocParsingTextFallsBackToMarkdownTextField(t *testing.T) {
	result := &paddleocr.DocParsingResult{
		Pages: []paddleocr.DocParsingPage{
			{Markdown: map[string]interface{}{"text": "markdown.text 内容"}},
		},
	}

	if got := extractDocParsingText(result); got != "markdown.text 内容" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDocParsingTextFallsBackToPrunedResult(t *testing.T) {
	result := &paddleocr.DocParsingResult{
		Pages: []paddleocr.DocParsingPage{
			{
				PrunedResult: map[string]interface{}{
					"blocks": []interface{}{
						map[string]interface{}{"text": "段落 A"},
						map[string]interface{}{"content": "段落 B"},
					},
				},
			},
		},
	}

	if got := extractDocParsingText(result); got != "段落 A\n段落 B" {
		t.Fatalf("got %q", got)
	}
}

func TestParsePaddleDocumentJSONLRelaxedReportsMissingMarkdownPages(t *testing.T) {
	result, missingPages, err := parsePaddleDocumentJSONLRelaxed("job-doc", []map[string]interface{}{
		{
			"result": map[string]interface{}{
				"layoutParsingResults": []interface{}{
					map[string]interface{}{"markdown": map[string]interface{}{"text": "第一页"}},
					map[string]interface{}{"prunedResult": map[string]interface{}{"blocks": []interface{}{map[string]interface{}{"text": "第二页兜底文本"}}}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("parsePaddleDocumentJSONLRelaxed returned error: %v", err)
	}
	if got := formatPageNumbers(missingPages); got != "[2]" {
		t.Fatalf("missing pages = %s, want [2]", got)
	}
	if got := extractDocParsingText(result); !strings.Contains(got, "第一页") || !strings.Contains(got, "第二页兜底文本") {
		t.Fatalf("unexpected text: %q", got)
	}
}

func TestExtractPDFTextWithOCRReturnsAPIError(t *testing.T) {
	withOCRConfig(t, "test-token", paddleocr.PaddleOCRVL16, 2)
	parser := &stubPaddleDocParser{submitErr: fmt.Errorf("api unavailable")}
	withPaddleDocParser(t, parser)

	_, err := extractPDFTextWithOCR("scan.pdf")
	if err == nil {
		t.Fatal("expected api error")
	}
	if !strings.Contains(err.Error(), "PaddleOCR document parsing submit failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractPDFTextWithOCRExplainsMissingMarkdownText(t *testing.T) {
	withOCRConfig(t, "test-token", paddleocr.PaddleOCRVL16, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"result":{"layoutParsingResults":[{"markdown":{"text":"第一页"}},{"prunedResult":{"blocks":[{"text":"第二页兜底文本"}]}}]}}`)
	}))
	defer server.Close()

	parser := &stubPaddleDocParser{
		job: &paddleocr.Job{JobID: "job-doc"},
		status: &paddleocr.JobStatus{
			JobID:     "job-doc",
			State:     "done",
			ResultURL: map[string]string{"jsonUrl": server.URL},
		},
	}
	withPaddleDocParser(t, parser)

	got, err := extractPDFTextWithOCR("scan.pdf")
	if err != nil {
		t.Fatalf("missing markdown.text page should fall back to prunedResult: %v", err)
	}
	if !strings.Contains(got, "第一页") || !strings.Contains(got, "第二页兜底文本") {
		t.Fatalf("got %q", got)
	}
}

func TestExtractPDFTextWithFallbackReturnsCombinedErrorWhenOCRTextIsGarbledAndNativeFails(t *testing.T) {
	originalNative := nativePDFTextExtractor
	originalOCR := ocrPDFTextExtractor
	defer func() {
		nativePDFTextExtractor = originalNative
		ocrPDFTextExtractor = originalOCR
	}()

	nativePDFTextExtractor = func(filePath string) (string, error) {
		return "", fmt.Errorf("native failed")
	}
	ocrPDFTextExtractor = func(filePath string) (string, error) {
		return "abc" + strings.Repeat("\u008c", 2), nil
	}

	_, err := extractPDFTextWithFallback("garbled-ocr.pdf")
	if err == nil {
		t.Fatal("expected combined error")
	}
	if !strings.Contains(err.Error(), "garbled content ratio too high") || !strings.Contains(err.Error(), "native failed") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSplitTextIntoChunksReturnsNilForBlankText(t *testing.T) {
	if chunks := splitTextIntoChunks(" \n\t "); chunks != nil {
		t.Fatalf("blank text should return nil, got %#v", chunks)
	}
}

func TestSplitTextIntoChunksKeepsSmallTextAsSingleChunk(t *testing.T) {
	text := "# 标题\n\n这是一段较短的中文内容。\n\nThis is a short paragraph."

	chunks := splitTextIntoChunks(text)
	if len(chunks) != 1 {
		t.Fatalf("small text should produce one chunk, got %d: %#v", len(chunks), chunks)
	}
	if chunks[0] != text {
		t.Fatalf("small text changed: got %q", chunks[0])
	}
}

func TestSplitTextIntoChunksCombinesMarkdownHeadingsAndParagraphs(t *testing.T) {
	text := "# 第一章\n\n第一段内容。\n\n## 第二节\n\n第二段内容。"

	chunks := splitTextIntoChunks(text)
	if len(chunks) != 1 {
		t.Fatalf("expected one combined chunk, got %d: %#v", len(chunks), chunks)
	}
	for _, want := range []string{"# 第一章", "第一段内容。", "## 第二节", "第二段内容。"} {
		if !strings.Contains(chunks[0], want) {
			t.Fatalf("combined chunk missing %q: %q", want, chunks[0])
		}
	}
}

func TestSplitTextIntoChunksSplitsLongParagraphAtSentenceBoundaries(t *testing.T) {
	text := strings.Repeat("甲", 1000) + "。" +
		strings.Repeat("乙", 1000) + "。" +
		strings.Repeat("丙", 1000) + "。"

	chunks := splitTextIntoChunks(text)
	if len(chunks) != 3 {
		t.Fatalf("expected sentence chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if runeLen(chunk) > defaultChunkSize {
			t.Fatalf("chunk %d exceeds max size: %d", i, runeLen(chunk))
		}
		if !strings.HasSuffix(chunk, "。") {
			runes := []rune(chunk)
			start := len(runes) - 20
			if start < 0 {
				start = 0
			}
			t.Fatalf("chunk %d should end at sentence punctuation: %q", i, string(runes[start:]))
		}
		if strings.ContainsRune(chunk, unicode.ReplacementChar) {
			t.Fatalf("chunk %d contains replacement char: %q", i, chunk)
		}
	}
}

func TestSplitTextIntoChunksOverlapPrefersSentenceBoundary(t *testing.T) {
	firstParagraph := strings.Repeat("甲", 1850) + "。关键尾句。"
	secondParagraph := strings.Repeat("下一段内容", 40) + "。"

	chunks := splitTextIntoChunks(firstParagraph + "\n\n" + secondParagraph)
	if len(chunks) != 2 {
		t.Fatalf("expected two chunks, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[1], "关键尾句。\n\n下一段内容") {
		t.Fatalf("overlap should start at sentence boundary, got prefix %q", []rune(chunks[1])[:20])
	}
	if runeLen(chunks[1]) > defaultChunkSize {
		t.Fatalf("overlap chunk exceeds max size: %d", runeLen(chunks[1]))
	}
}

func TestSplitTextIntoChunksFallsBackToRuneWindowWithoutBreakingUTF8(t *testing.T) {
	text := strings.Repeat("中文🙂", 900)

	chunks := splitTextIntoChunks(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for long text, got %d", len(chunks))
	}
	joined := strings.Join(chunks, "")
	for i, chunk := range chunks {
		if runeLen(chunk) > defaultChunkSize {
			t.Fatalf("chunk %d exceeds max size: %d", i, runeLen(chunk))
		}
		if strings.ContainsRune(chunk, unicode.ReplacementChar) {
			t.Fatalf("chunk %d contains replacement char: %q", i, chunk)
		}
	}
	if strings.ContainsRune(joined, unicode.ReplacementChar) {
		t.Fatalf("joined chunks contain replacement char")
	}
}

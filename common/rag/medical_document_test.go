package rag

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/PaddlePaddle/PaddleOCR/api_sdk/go"
)

func TestMedicalImageUsesDocumentOCR(t *testing.T) {
	withOCRConfig(t, "test-token", paddleocr.PaddleOCRVL16, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"result":{"layoutParsingResults":[{"markdown":{"text":"白细胞 11.2 10^9/L"}}]}}`)
	}))
	defer server.Close()
	parser := &stubPaddleDocParser{job: &paddleocr.Job{JobID: "image"}, status: &paddleocr.JobStatus{JobID: "image", State: "done", ResultURL: map[string]string{"jsonUrl": server.URL}}}
	withPaddleDocParser(t, parser)
	text, err := ExtractMedicalDocument(context.Background(), "report.png")
	if err != nil || text != "白细胞 11.2 10^9/L" {
		t.Fatal(text, err)
	}
	if parser.req.FilePath != "report.png" {
		t.Fatal("image was not passed to parser")
	}
}

func TestMedicalTextNeedsNoOCR(t *testing.T) {
	withOCRConfig(t, "", paddleocr.PaddleOCRVL16, 2)
	path := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(path, []byte("合成报告"), 0600); err != nil {
		t.Fatal(err)
	}
	text, err := ExtractMedicalDocument(context.Background(), path)
	if err != nil || text != "合成报告" {
		t.Fatal(text, err)
	}
	if _, err = ExtractMedicalDocument(context.Background(), "scan.jpg"); err == nil {
		t.Fatal("image accepted without OCR token")
	}
}

package rag

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testFileAEnv          = "TEST_FILE_A_PATH"
	testFileAOutputDirEnv = "TEST_FILE_A_OUTPUT_DIR"
	defaultTestOutputDir  = "outputs/pdf_parse_compare"
)

func TestParseTestFileAWithoutChunking(t *testing.T) {
	restoreWorkingDir := chdirProjectRoot(t)
	defer restoreWorkingDir()

	filePath := resolveTestFileAPath(t)
	t.Logf("parse file without chunking: %s", filePath)

	nativeText, err := extractPDFText(filePath)
	if err != nil {
		t.Fatalf("pdftotext-go parse failed: %v", err)
	}
	nativeText = strings.TrimSpace(nativeText)
	if nativeText == "" {
		t.Fatal("pdftotext-go parse returned empty text")
	}
	t.Logf("pdftotext-go text bytes=%d preview=%q", len(nativeText), previewForLog(nativeText))

	ocrText, err := extractPDFTextWithOCR(filePath)
	if err != nil {
		t.Fatalf("PaddleOCR parse failed: %v", err)
	}
	ocrText = strings.TrimSpace(ocrText)
	if ocrText == "" {
		t.Fatal("PaddleOCR parse returned empty text")
	}
	t.Logf("PaddleOCR text bytes=%d preview=%q", len(ocrText), previewForLog(ocrText))

	outputDir := resolveTestFileAOutputDir()
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatalf("create output directory: %v", err)
	}
	nativeOutputPath := filepath.Join(outputDir, "测试文件A_pdftotext-go.txt")
	if err := os.WriteFile(nativeOutputPath, []byte(nativeText), 0644); err != nil {
		t.Fatalf("write pdftotext-go output: %v", err)
	}
	ocrOutputPath := filepath.Join(outputDir, "测试文件A_paddleocr.txt")
	if err := os.WriteFile(ocrOutputPath, []byte(ocrText), 0644); err != nil {
		t.Fatalf("write PaddleOCR output: %v", err)
	}
	t.Logf("pdftotext-go output saved: %s", nativeOutputPath)
	t.Logf("PaddleOCR output saved: %s", ocrOutputPath)
}

func resolveTestFileAPath(t *testing.T) string {
	t.Helper()

	if path := strings.TrimSpace(os.Getenv(testFileAEnv)); path != "" {
		return requireExistingPDF(t, path)
	}

	candidates := []string{
		"测试文件A.pdf",
		filepath.Join("testdata", "测试文件A.pdf"),
		filepath.Join("common", "rag", "testdata", "测试文件A.pdf"),
		filepath.Join("uploads", "测试文件A.pdf"),
		filepath.Join("uploads", "stored", "测试文件A.pdf"),
	}
	for _, candidate := range candidates {
		if path, ok := existingPDF(candidate); ok {
			return path
		}
	}

	t.Skipf("测试文件A not found; set %s=/path/to/测试文件A.pdf to run this integration test", testFileAEnv)
	return ""
}

func requireExistingPDF(t *testing.T, path string) string {
	t.Helper()

	if resolved, ok := existingPDF(path); ok {
		return resolved
	}
	t.Fatalf("%s does not point to an existing PDF: %s", testFileAEnv, path)
	return ""
}

func resolveTestFileAOutputDir() string {
	if path := strings.TrimSpace(os.Getenv(testFileAOutputDirEnv)); path != "" {
		return path
	}
	return defaultTestOutputDir
}

func existingPDF(path string) (string, bool) {
	if strings.ToLower(filepath.Ext(path)) != ".pdf" {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	return path, true
}

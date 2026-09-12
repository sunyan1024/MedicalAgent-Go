package rag

import (
	"GoAI/config"
	"context"
	"fmt"
	"github.com/PaddlePaddle/PaddleOCR/api_sdk/go"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ExtractMedicalDocument reuses the document parser for PDF and image reports.
func ExtractMedicalDocument(ctx context.Context, path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".txt" {
		b, err := os.ReadFile(path)
		return string(b), err
	}
	if ext == ".pdf" {
		if text, err := extractPDFText(path); err == nil {
			return text, nil
		}
	}
	c := config.GetConfig().RagModelConfig
	if strings.TrimSpace(c.OCRAccessToken) == "" {
		return "", fmt.Errorf("请配置 ragModelConfig.ocrAccessToken，或粘贴报告文字")
	}
	m := strings.TrimSpace(c.OCRModel)
	if m == "" {
		m = defaultOCRModel
	}
	if !paddleocr.IsDocumentParsingModel(m) {
		return "", fmt.Errorf("OCR 配置不是文档解析模型")
	}
	seconds := c.OCRTimeoutSeconds
	if seconds <= 0 {
		seconds = defaultOCRTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(seconds)*time.Second)
	defer cancel()
	client, err := newPaddleDocParser(c.OCRAccessToken, time.Duration(seconds)*time.Second)
	if err != nil {
		return "", err
	}
	req := &paddleocr.DocParsingRequest{Model: m, FilePath: path, Options: docParsingOptionsForModel(m)}
	result, err := parsePaddleDocumentWithPageDiagnostics(ctx, client, req, path)
	if err != nil {
		return "", err
	}
	text := extractDocParsingText(result)
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("未识别到文字，请上传清晰报告或粘贴文字")
	}
	return text, nil
}

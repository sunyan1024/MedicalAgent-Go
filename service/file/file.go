package file

import (
	"GoAI/common/rag"
	fileDao "GoAI/dao/file"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
)

// 上传 rag 相关文件（这里只允许文本文件）
// 其实可以直接将其向量化进行保存，但这边依旧存储到服务器上以便后续可以在服务器上查看历史 RAG 文件
func UploadRagFile(username string, file *multipart.FileHeader) (*model.KnowledgeFile, error) {
	// 校验文件类型和文件名
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("[UploadRagFile] File validation failed: %v", err)
		return nil, err
	}

	// 创建用户目录
	userDir := filepath.Join("uploads", username)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		log.Printf("[UploadRagFile] Failed to create user directory %s: %v", userDir, err)
		return nil, err
	}

	// 生成UUID作为唯一文件名
	uuid := utils.GenerateUUID()

	ext := filepath.Ext(file.Filename)           // 提取文件扩展名
	filename := uuid + ext                       // 生成唯一文件名
	filePath := filepath.Join(userDir, filename) // 构建完整文件路径

	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		log.Printf("[UploadRagFile] Failed to open uploaded file: %v", err)
		return nil, err
	}
	defer src.Close()

	// 创建目标文件
	dst, err := os.Create(filePath)
	if err != nil {
		log.Printf("[UploadRagFile] Failed to create destination file %s: %v", filePath, err)
		return nil, err
	}
	defer dst.Close()

	// 保存文件到目标路径
	if _, err := io.Copy(dst, src); err != nil {
		log.Printf("[UploadRagFile] Failed to copy file content: %v", err)
		os.Remove(filePath)
		return nil, err
	}

	log.Printf("[UploadRagFile] File uploaded successfully: %s", filePath)

	// 创建 RAG 索引器并对文件进行向量化
	indexer, err := rag.NewRAGIndexer(filename)
	if err != nil {
		log.Printf("[UploadRagFile] Failed to create RAG indexer: %v", err)
		// 删除已上传的文件
		os.Remove(filePath)
		return nil, err
	}

	// 读取文件内容并创建向量索引
	if err := indexer.IndexFile(context.Background(), filePath); err != nil {
		log.Printf("[UploadRagFile] Failed to index file: %v", err)
		// 删除已上传的文件和索引
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return nil, err
	}

	knowledgeFile := &model.KnowledgeFile{
		ID:           uuid,
		UserName:     username,
		OriginalName: file.Filename,
		StoredName:   filename,
		FilePath:     filePath,
		Size:         file.Size,
	}
	if _, err := fileDao.CreateKnowledgeFile(knowledgeFile); err != nil {
		log.Printf("[UploadRagFile] Failed to create knowledge file record: %v", err)
		os.Remove(filePath)
		rag.DeleteIndex(context.Background(), filename)
		return nil, err
	}

	log.Printf("[UploadRagFile] File indexed successfully: %s", filename)
	return knowledgeFile, nil
}

func ListKnowledgeFiles(username string) ([]model.KnowledgeFile, error) {
	return fileDao.GetKnowledgeFilesByUserName(username)
}

func DeleteKnowledgeFile(username string, fileID string) error {
	if fileID == "" {
		return fmt.Errorf("文件ID不能为空")
	}

	knowledgeFile, err := fileDao.GetKnowledgeFileByID(username, fileID)
	if err != nil {
		return err
	}

	if err := rag.DeleteIndex(context.Background(), knowledgeFile.StoredName); err != nil {
		return err
	}

	if err := os.Remove(knowledgeFile.FilePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return fileDao.DeleteKnowledgeFileByID(username, fileID)
}

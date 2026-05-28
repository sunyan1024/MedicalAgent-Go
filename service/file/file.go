package file

import (
	"GoAI/common/rag"
	fileDao "GoAI/dao/file"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

var ErrDuplicateFile = errors.New("文件已存在，请勿重复上传")

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

	uuid := utils.GenerateUUID()
	tempPath := filepath.Join(userDir, "."+uuid+".uploading")

	ext := strings.ToLower(filepath.Ext(file.Filename)) // 提取文件扩展名

	// 打开上传的文件
	src, err := file.Open()
	if err != nil {
		log.Printf("[UploadRagFile] Failed to open uploaded file: %v", err)
		return nil, err
	}
	defer src.Close()

	// 先写入临时文件并同步计算 MD5，避免重复读取上传流。
	dst, err := os.Create(tempPath)
	if err != nil {
		log.Printf("[UploadRagFile] Failed to create temp file %s: %v", tempPath, err)
		return nil, err
	}

	hash := md5.New()
	if _, err := io.Copy(io.MultiWriter(dst, hash), src); err != nil {
		log.Printf("[UploadRagFile] Failed to copy file content: %v", err)
		dst.Close()
		os.Remove(tempPath)
		return nil, err
	}
	if err := dst.Close(); err != nil {
		log.Printf("[UploadRagFile] Failed to close temp file %s: %v", tempPath, err)
		os.Remove(tempPath)
		return nil, err
	}

	fileMD5 := hex.EncodeToString(hash.Sum(nil))
	filename := fileMD5 + ext
	filePath := filepath.Join(userDir, filename)

	if _, err := fileDao.GetKnowledgeFileByUserNameAndStoredName(username, filename); err == nil {
		os.Remove(tempPath)
		return nil, ErrDuplicateFile
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("[UploadRagFile] Failed to check duplicate file username=%s stored_name=%s: %v", username, filename, err)
		os.Remove(tempPath)
		return nil, err
	}

	if _, err := os.Stat(filePath); err == nil {
		os.Remove(tempPath)
		return nil, ErrDuplicateFile
	} else if err != nil && !os.IsNotExist(err) {
		log.Printf("[UploadRagFile] Failed to stat destination file %s: %v", filePath, err)
		os.Remove(tempPath)
		return nil, err
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		log.Printf("[UploadRagFile] Failed to rename temp file %s to %s: %v", tempPath, filePath, err)
		os.Remove(tempPath)
		return nil, err
	}

	log.Printf("[UploadRagFile] File uploaded successfully: %s", filePath)

	// 创建 RAG 索引器并对文件进行向量化
	indexKey := rag.BuildFileIndexKey(username, filename)
	indexer, err := rag.NewRAGIndexer(indexKey)
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
		rag.DeleteIndex(context.Background(), indexKey)
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
		rag.DeleteIndex(context.Background(), indexKey)
		if strings.Contains(err.Error(), "idx_user_stored_name") || strings.Contains(err.Error(), "Duplicate entry") {
			return nil, ErrDuplicateFile
		}
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

	indexKey := rag.BuildFileIndexKey(username, knowledgeFile.StoredName)
	if err := rag.DeleteIndex(context.Background(), indexKey); err != nil {
		return err
	}

	if err := os.Remove(knowledgeFile.FilePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return fileDao.DeleteKnowledgeFileByID(username, fileID)
}

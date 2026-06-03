package file

import (
	"GoAI/common/mysql"
	"GoAI/common/rag"
	redisPkg "GoAI/common/redis"
	fileDao "GoAI/dao/file"
	"GoAI/model"
	"GoAI/utils"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrDuplicateFile = errors.New("文件已存在，请勿重复上传")
	ErrHashMismatch  = errors.New("文件 SHA-256 校验失败，请重新上传")
	ErrFileIndexing  = errors.New("文件正在处理中，请稍后重试")
	sha256Pattern    = regexp.MustCompile(`^[a-fA-F0-9]{64}$`)
)

type PrecheckResult struct {
	NeedUpload bool                 `json:"need_upload"`
	Reused     bool                 `json:"reused"`
	File       *model.KnowledgeFile `json:"file,omitempty"`
}

func PrecheckRagFile(username string, originalName string, fileSHA256 string, size int64) (*PrecheckResult, error) {
	ext, storedName, err := validatePrecheckInput(originalName, fileSHA256, size)
	if err != nil {
		return nil, err
	}

	if err := ensureUserDoesNotHaveFile(username, storedName); err != nil {
		return nil, err
	}

	storedFile, err := fileDao.GetStoredFileBySHA256AndExt(strings.ToLower(fileSHA256), ext)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &PrecheckResult{NeedUpload: true}, nil
	}
	if err != nil {
		return nil, err
	}

	knowledgeFile, err := createUserFileFromStoredFile(username, originalName, storedName, size, storedFile)
	if err != nil {
		return nil, err
	}

	return &PrecheckResult{
		NeedUpload: false,
		Reused:     true,
		File:       knowledgeFile,
	}, nil
}

// 上传 RAG 文件。客户端 SHA-256 只用于提前发现传输错误，服务端计算值是最终可信值。
func UploadRagFile(username string, file *multipart.FileHeader, clientSHA256 string) (*model.KnowledgeFile, error) {
	if err := utils.ValidateFile(file); err != nil {
		log.Printf("[UploadRagFile] File validation failed: %v", err)
		return nil, err
	}
	clientSHA256 = strings.ToLower(strings.TrimSpace(clientSHA256))
	if !sha256Pattern.MatchString(clientSHA256) {
		return nil, fmt.Errorf("文件 SHA-256 格式不正确")
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	storedName := clientSHA256 + ext
	if err := ensureUserDoesNotHaveFile(username, storedName); err != nil {
		return nil, err
	}

	tempDir := filepath.Join("uploads", "tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, err
	}
	tempPath := filepath.Join(tempDir, "."+utils.GenerateUUID()+".uploading")

	serverSHA256, err := saveUploadedFileAndHash(file, tempPath)
	if err != nil {
		return nil, err
	}
	if serverSHA256 != clientSHA256 {
		os.Remove(tempPath)
		return nil, ErrHashMismatch
	}

	storedDir := filepath.Join("uploads", "stored")
	if err := os.MkdirAll(storedDir, 0755); err != nil {
		os.Remove(tempPath)
		return nil, err
	}

	createdStoredFile := false
	storedFile, err := fileDao.GetStoredFileBySHA256AndExt(serverSHA256, ext)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		storedFile, createdStoredFile, err = createStoredFileFromTemp(serverSHA256, ext, tempPath, storedDir, file.Size)
		if err != nil {
			os.Remove(tempPath)
			return nil, err
		}
	} else {
		os.Remove(tempPath)
		if err != nil {
			return nil, err
		}
	}

	knowledgeFile, err := createUserFileFromStoredFile(username, file.Filename, storedName, file.Size, storedFile)
	if err != nil {
		if createdStoredFile {
			os.Remove(storedFile.FilePath)
			fileDao.HardDeleteStoredFileByID(mysql.DB, storedFile.ID)
		}
		return nil, err
	}

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
	storedFile, err := fileDao.GetStoredFileByID(knowledgeFile.StoredFileID)
	if err != nil {
		return err
	}

	deleteStoredIndex := false
	if err := mysql.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("user_name = ? AND id = ?", username, fileID).Delete(&model.KnowledgeFile{}).Error; err != nil {
			return err
		}
		if err := fileDao.DecrementStoredFileRefCount(tx, storedFile.ID); err != nil {
			return err
		}

		var current model.StoredFile
		if err := tx.Where("id = ?", storedFile.ID).First(&current).Error; err != nil {
			return err
		}
		if current.RefCount == 0 {
			if err := fileDao.DeleteStoredFileByID(tx, storedFile.ID); err != nil {
				return err
			}
			deleteStoredIndex = true
		}
		return nil
	}); err != nil {
		return err
	}

	if deleteStoredIndex {
		indexKey := rag.BuildStoredFileIndexKey(storedFile.ID)
		if err := rag.DeleteIndex(context.Background(), indexKey); err != nil {
			return err
		}
	}

	return nil
}

func validatePrecheckInput(originalName string, fileSHA256 string, size int64) (string, string, error) {
	ext := strings.ToLower(filepath.Ext(originalName))
	if ext != ".md" && ext != ".txt" && ext != ".pdf" {
		return "", "", fmt.Errorf("文件类型不正确，只允许 .md、.txt 或 .pdf 文件，当前扩展名: %s", ext)
	}
	fileSHA256 = strings.ToLower(strings.TrimSpace(fileSHA256))
	if !sha256Pattern.MatchString(fileSHA256) {
		return "", "", fmt.Errorf("文件 SHA-256 格式不正确")
	}
	if size < 0 {
		return "", "", fmt.Errorf("文件大小不正确")
	}
	return ext, fileSHA256 + ext, nil
}

func ensureUserDoesNotHaveFile(username string, storedName string) error {
	if _, err := fileDao.GetKnowledgeFileByUserNameAndStoredName(username, storedName); err == nil {
		return ErrDuplicateFile
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return nil
}

func saveUploadedFileAndHash(file *multipart.FileHeader, tempPath string) (string, error) {
	src, err := file.Open()
	if err != nil {
		return "", err
	}
	defer src.Close()

	dst, err := os.Create(tempPath)
	if err != nil {
		return "", err
	}

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(dst, hash), src); err != nil {
		dst.Close()
		os.Remove(tempPath)
		return "", err
	}
	if err := dst.Close(); err != nil {
		os.Remove(tempPath)
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func createStoredFileFromTemp(fileSHA256 string, ext string, tempPath string, storedDir string, size int64) (*model.StoredFile, bool, error) {
	storedName := fileSHA256 + ext
	filePath := filepath.Join(storedDir, storedName)
	if err := os.Rename(tempPath, filePath); err != nil {
		if _, statErr := os.Stat(filePath); statErr == nil {
			storedFile, lookupErr := fileDao.GetStoredFileBySHA256AndExt(fileSHA256, ext)
			return storedFile, false, lookupErr
		}
		return nil, false, err
	}

	storedFile := &model.StoredFile{
		ID:          utils.GenerateUUID(),
		FileSHA256:  fileSHA256,
		Ext:         ext,
		StoredName:  storedName,
		FilePath:    filePath,
		Size:        size,
		RefCount:    0,
		IndexStatus: model.StoredFileIndexStatusPending,
	}
	if _, err := fileDao.CreateStoredFile(storedFile); err != nil {
		if strings.Contains(err.Error(), "idx_sha256_ext") || strings.Contains(err.Error(), "Duplicate entry") {
			storedFile, lookupErr := fileDao.GetStoredFileBySHA256AndExt(fileSHA256, ext)
			return storedFile, false, lookupErr
		}
		os.Remove(filePath)
		return nil, false, err
	}
	return storedFile, true, nil
}

func createUserFileFromStoredFile(username string, originalName string, storedName string, size int64, storedFile *model.StoredFile) (*model.KnowledgeFile, error) {
	if err := ensureStoredFileIndexed(storedFile); err != nil {
		return nil, err
	}

	knowledgeFile := &model.KnowledgeFile{
		ID:           utils.GenerateUUID(),
		UserName:     username,
		OriginalName: originalName,
		StoredName:   storedName,
		StoredFileID: storedFile.ID,
		Size:         size,
	}
	if err := mysql.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(knowledgeFile).Error; err != nil {
			return err
		}
		return fileDao.IncrementStoredFileRefCount(tx, storedFile.ID)
	}); err != nil {
		if strings.Contains(err.Error(), "idx_user_stored_name") || strings.Contains(err.Error(), "Duplicate entry") {
			return nil, ErrDuplicateFile
		}
		return nil, err
	}

	return knowledgeFile, nil
}

func ensureStoredFileIndexed(storedFile *model.StoredFile) error {
	if storedFile == nil {
		return fmt.Errorf("stored file is nil")
	}
	if storedFile.IndexStatus == model.StoredFileIndexStatusReady {
		return nil
	}

	ctx := context.Background()
	lockKey := fmt.Sprintf("rag:index:lock:%s", storedFile.ID)
	lockToken := utils.GenerateUUID()
	locked, err := redisPkg.AcquireLock(ctx, lockKey, lockToken, 10*time.Minute)
	if err != nil {
		return normalizeRAGIndexError(err)
	}
	if !locked {
		return ErrFileIndexing
	}
	defer func() {
		if err := redisPkg.ReleaseLock(ctx, lockKey, lockToken); err != nil {
			log.Printf("[ensureStoredFileIndexed] release lock failed stored_file_id=%s err=%v", storedFile.ID, err)
		}
	}()

	latest, err := fileDao.GetStoredFileByIDUnscoped(storedFile.ID)
	if err != nil {
		return err
	}
	if latest.IndexStatus == model.StoredFileIndexStatusReady {
		return nil
	}

	indexKey := rag.BuildStoredFileIndexKey(latest.ID)
	if err := fileDao.UpdateStoredFileIndexPending(latest.ID); err != nil {
		return err
	}

	indexer, err := rag.NewRAGIndexer(indexKey)
	if err != nil {
		fileDao.UpdateStoredFileIndexFailed(latest.ID, err.Error())
		return normalizeRAGIndexError(err)
	}
	if err := indexer.IndexFile(ctx, latest.FilePath); err != nil {
		rag.DeleteIndex(ctx, indexKey)
		fileDao.UpdateStoredFileIndexFailed(latest.ID, err.Error())
		return normalizeRAGIndexError(err)
	}

	if err := fileDao.UpdateStoredFileIndexReady(latest.ID, time.Now()); err != nil {
		return err
	}
	return nil
}

func normalizeRAGIndexError(err error) error {
	if err == nil {
		return nil
	}

	msg := err.Error()
	switch {
	case strings.Contains(msg, "RediSearch") ||
		strings.Contains(msg, "redis index") ||
		strings.Contains(msg, "FT.INFO") ||
		strings.Contains(msg, "FT.CREATE"):
		return fmt.Errorf("知识库索引服务不可用，请检查 Redis Stack/RediSearch: %w", err)
	case strings.Contains(msg, "failed to extract pdf text") ||
		strings.Contains(msg, "failed to open pdf file") ||
		strings.Contains(msg, "failed to read pdf text"):
		return fmt.Errorf("PDF 文本解析失败: %w", err)
	case strings.Contains(msg, "failed to create embedder") ||
		strings.Contains(msg, "failed to store document") ||
		strings.Contains(strings.ToLower(msg), "embedding"):
		return fmt.Errorf("文档向量化失败，请检查模型配置或网络: %w", err)
	case strings.Contains(msg, "empty content") ||
		strings.Contains(msg, "无法提取文本"):
		return fmt.Errorf("文件内容为空或无法提取文本: %w", err)
	default:
		return fmt.Errorf("知识库文件解析或索引失败: %w", err)
	}
}

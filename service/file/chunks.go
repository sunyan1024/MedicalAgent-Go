package file

import (
	"context"
	"time"

	"GoAI/common/rag"
	redisPkg "GoAI/common/redis"
	fileDao "GoAI/dao/file"
	"GoAI/model"
	"GoAI/utils"
)

// WithEnterpriseChunks resolves access through the enterprise KnowledgeFile and
// serializes with upload/reuse/deletion of the same shared physical file.
func WithEnterpriseChunks(ctx context.Context, uid, fileID string, fn func(*rag.ChunkStore, string, string) error) error {
	owner := model.EnterpriseKnowledgeOwner(uid)
	file, err := fileDao.GetKnowledgeFileByID(owner, fileID)
	if err != nil {
		return err
	}
	key, token := "rag:lifecycle:lock:"+file.StoredFileID, utils.GenerateUUID()
	locked, err := redisPkg.AcquireLock(ctx, key, token, 10*time.Minute)
	if err != nil {
		return err
	}
	if !locked {
		return ErrFileIndexing
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = redisPkg.ReleaseLock(releaseCtx, key, token)
	}()
	// The file reference may have been removed while acquiring the lock.
	file, err = fileDao.GetKnowledgeFileByID(owner, fileID)
	if err != nil {
		return err
	}
	stored, err := fileDao.GetStoredFileByID(file.StoredFileID)
	if err != nil {
		return err
	}
	if stored.IndexStatus != model.StoredFileIndexStatusReady {
		return ErrFileIndexing
	}
	indexKey := rag.BuildStoredFileIndexKey(stored.ID)
	if err := redisPkg.Rdb.Do(ctx, "FT.INFO", redisPkg.GenerateIndexName(indexKey)).Err(); err != nil {
		return err
	}
	store := &rag.ChunkStore{Client: redisPkg.Rdb, IndexKey: indexKey, FileSHA256: stored.FileSHA256, Source: stored.FilePath}
	if err := fn(store, key, token); err != nil {
		return err
	}
	current, err := redisPkg.Rdb.Get(ctx, key).Result()
	if err != nil {
		return err
	}
	if current != token {
		return rag.ErrChunkConflict
	}
	return nil
}

package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"testing"

	appmysql "GoAI/common/mysql"
	"GoAI/common/rag"
	redisPkg "GoAI/common/redis"
	fileDao "GoAI/dao/file"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils"
)

func TestSharedKnowledgeFileLifecycle(t *testing.T) {
	if os.Getenv("GOPHERAI_TEST_REDIS") != "1" {
		t.Skip("set GOPHERAI_TEST_MYSQL=1 GOPHERAI_TEST_REDIS=1 for shared file lifecycle integration")
	}
	db := testutil.MySQL(t)
	previousDB, previousRedis := appmysql.DB, redisPkg.Rdb
	appmysql.DB = db
	client := testutil.RedisSearch(t)
	redisPkg.Rdb = client
	t.Cleanup(func() { appmysql.DB, redisPkg.Rdb = previousDB, previousRedis; client.Close() })
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.StoredFile{}, &model.KnowledgeFile{}); err != nil {
		t.Fatal(err)
	}
	id := utils.GenerateUUID()
	hash := sha256.Sum256([]byte(id))
	checksum := hex.EncodeToString(hash[:])
	stored := model.StoredFile{ID: id, FileSHA256: checksum, Ext: ".txt", StoredName: checksum + ".txt", FilePath: "unused-ready-fixture.txt", Size: 1, IndexStatus: model.StoredFileIndexStatusReady}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	indexKey := rag.BuildStoredFileIndexKey(id)
	indexName := redisPkg.GenerateIndexName(indexKey)
	prefix := "gopherai_test:" + id + ":"
	if err := client.Do(ctx, "FT.CREATE", indexName, "ON", "HASH", "PREFIX", 1, prefix, "SCHEMA", "content", "TEXT").Err(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Do(ctx, "FT.DROPINDEX", indexName, "DD") })
	alice := model.PersonalKnowledgeOwner("alice")
	enterprise := model.EnterpriseKnowledgeOwner("admin")
	personal, err := PrecheckRagFile(alice, "Personal.txt", checksum, 1)
	if err != nil || !personal.Reused {
		t.Fatalf("personal reuse: result=%+v err=%v", personal, err)
	}
	shared, err := PrecheckRagFile(enterprise, "Enterprise.txt", checksum, 1)
	if err != nil || !shared.Reused {
		t.Fatalf("enterprise reuse: result=%+v err=%v", shared, err)
	}
	if _, err := PrecheckRagFile(model.EnterpriseKnowledgeOwner("admin-two"), "Other.txt", checksum, 1); !errors.Is(err, ErrDuplicateFile) {
		t.Fatalf("enterprise dedup: %v", err)
	}
	checkCount := func(want int) {
		t.Helper()
		current, err := fileDao.GetStoredFileByIDUnscoped(id)
		if err != nil || current.RefCount != want {
			t.Fatalf("ref count: %+v err=%v want=%d", current, err, want)
		}
	}
	checkCount(2)
	release, err := lockStoredFileLifecycle(id)
	if err != nil {
		t.Fatal(err)
	}
	_, attachErr := PrecheckRagFile(model.PersonalKnowledgeOwner("bob"), "Bob.txt", checksum, 1)
	deleteErr := DeleteKnowledgeFile(alice, personal.File.ID)
	release()
	if !errors.Is(attachErr, ErrFileIndexing) || !errors.Is(deleteErr, ErrFileIndexing) {
		t.Fatalf("concurrent changes were not serialized: %v / %v", attachErr, deleteErr)
	}
	checkCount(2)
	if err := DeleteKnowledgeFile(model.PersonalKnowledgeOwner("bob"), personal.File.ID); err == nil {
		t.Fatal("cross-owner delete succeeded")
	}
	checkCount(2)
	if err := DeleteKnowledgeFile(alice, personal.File.ID); err != nil {
		t.Fatal(err)
	}
	checkCount(1)
	if err := client.Do(ctx, "FT.INFO", indexName).Err(); err != nil {
		t.Fatal("shared index removed too early", err)
	}
	if err := DeleteKnowledgeFile(alice, personal.File.ID); err == nil {
		t.Fatal("repeated delete succeeded")
	}
	checkCount(1)
	if err := DeleteKnowledgeFile(model.EnterpriseKnowledgeOwner("admin-two"), shared.File.ID); err != nil {
		t.Fatal(err)
	}
	checkCount(0)
	if err := client.Do(ctx, "FT.INFO", indexName).Err(); err == nil {
		t.Fatal("unreferenced index still exists")
	}
	current, err := fileDao.GetStoredFileByIDUnscoped(id)
	if err != nil || !current.DeletedAt.Valid || current.IndexStatus != model.StoredFileIndexStatusPending {
		t.Fatalf("final state: %+v err=%v", current, err)
	}
}

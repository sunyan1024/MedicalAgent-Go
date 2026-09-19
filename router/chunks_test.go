package router

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	appmysql "GoAI/common/mysql"
	"GoAI/common/rag"
	redisPkg "GoAI/common/redis"
	"GoAI/internal/testutil"
	"GoAI/model"
	fileService "GoAI/service/file"
	"GoAI/utils/myjwt"
)

func TestEnterpriseChunkManagementAPI(t *testing.T) {
	db := testutil.MySQL(t)
	client := testutil.RedisStack(t)
	ctx := context.Background()
	oldDB, oldRedis := appmysql.DB, redisPkg.Rdb
	appmysql.DB, redisPkg.Rdb = db, client
	t.Cleanup(func() { appmysql.DB, redisPkg.Rdb = oldDB, oldRedis })
	if err := db.AutoMigrate(&model.User{}, &model.StoredFile{}, &model.KnowledgeFile{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.User{UID: "chunk-admin", Email: "chunks@example.com", Role: model.RoleAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("b", 64)
	stored := model.StoredFile{ID: "shared-chunks", FileSHA256: hash, Ext: ".txt", StoredName: hash + ".txt", FilePath: "/private/fixture.txt", IndexStatus: model.StoredFileIndexStatusReady, RefCount: 2}
	if err := db.Create(&stored).Error; err != nil {
		t.Fatal(err)
	}
	files := []model.KnowledgeFile{{ID: "enterprise-chunks", Scope: model.KnowledgeScopeEnterprise, OwnerKey: model.EnterpriseOwnerKey, UID: "chunk-admin", StoredFileID: stored.ID, StoredName: stored.StoredName, OriginalName: "医学资料.txt"}, {ID: "personal-chunks", Scope: model.KnowledgeScopePersonal, OwnerKey: "alice", UID: "alice", StoredFileID: stored.ID, StoredName: stored.StoredName, OriginalName: "我的资料.txt"}}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	indexKey := rag.BuildStoredFileIndexKey(stored.ID)
	if err := redisPkg.InitRedisIndex(ctx, indexKey, 2); err != nil {
		t.Fatal(err)
	}
	active := redisPkg.GenerateIndexNamePrefix(indexKey) + "doc_1"
	if err := client.HSet(ctx, active, "content", "共享医学切片", "chunk_idx", "1", "file_sha256", hash, "metadata", stored.FilePath, "vector", []byte{0, 0, 128, 63, 0, 0, 0, 0}).Err(); err != nil {
		t.Fatal(err)
	}
	token, err := myjwt.GenerateToken("chunk-admin")
	if err != nil {
		t.Fatal(err)
	}
	router := InitRouter()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v3/enterprise/file/"+path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	for _, path := range []string{"enterprise-chunks/chunks", "enterprise-chunks/chunks/doc_1"} {
		w := request("GET", path, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "共享医学切片") || strings.Contains(w.Body.String(), "/private/") || strings.Contains(w.Body.String(), "vector") {
			t.Fatalf("public projection: %s", w.Body.String())
		}
	}
	for _, path := range []string{"enterprise-chunks/chunks?page=0", "enterprise-chunks/chunks?page_size=101", "enterprise-chunks/chunks?enabled=invalid", "enterprise-chunks/chunks/doc_01"} {
		if w := request("GET", path, ""); w.Code != 400 {
			t.Fatalf("invalid parameters: %s", w.Body.String())
		}
	}
	if w := request("GET", "personal-chunks/chunks", ""); w.Code != 404 {
		t.Fatalf("personal file exposed: %s", w.Body.String())
	}
	if w := request("PATCH", "enterprise-chunks/chunks/doc_1", `{"enabled":false,"version":1}`); w.Code != 200 {
		t.Fatalf("disable: %s", w.Body.String())
	}
	if client.Exists(ctx, active).Val() != 0 || client.Exists(ctx, rag.DisabledChunkPrefix(indexKey)+"doc_1").Val() != 1 {
		t.Fatal("shared retrieval key not disabled")
	}
	w := request("GET", "enterprise-chunks/chunks?enabled=false&keyword=医学", "")
	var result struct {
		rag.ChunkPage
		StatusCode int `json:"status_code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Total != 1 || len(result.Chunks) != 1 || result.Chunks[0].Enabled {
		t.Fatalf("filtered list: %s %v", w.Body.String(), err)
	}
	if w := request("PATCH", "enterprise-chunks/chunks/doc_1", `{"enabled":true,"version":1}`); w.Code != 409 {
		t.Fatalf("stale version: %s", w.Body.String())
	}
	if w := request("PATCH", "enterprise-chunks/chunks/doc_1", `{"version":2}`); w.Code != 400 {
		t.Fatalf("missing enabled: %s", w.Body.String())
	}
	if w := request("PATCH", "enterprise-chunks/chunks/doc_1", `{"enabled":true,"version":2}`); w.Code != 200 {
		t.Fatalf("restore: %s", w.Body.String())
	}
	if w := request("DELETE", "enterprise-chunks/chunks/doc_1", `{"version":3}`); w.Code != 200 {
		t.Fatalf("delete: %s", w.Body.String())
	}
	if w := request("GET", "enterprise-chunks/chunks/doc_1", ""); w.Code != 404 {
		t.Fatalf("deleted detail: %s", w.Body.String())
	}
	// Use the same service resolution to test create/edit with a controlled embedding.
	err = fileService.WithEnterpriseChunks(ctx, "chunk-admin", "enterprise-chunks", func(store *rag.ChunkStore, key, lockToken string) error {
		store.Embed = func(context.Context, string) ([]float64, error) { return []float64{0, 1}, nil }
		created, e := store.Change(ctx, "create", "", rag.ChunkChange{Content: "新增共享正文"}, key, lockToken)
		if e != nil {
			return e
		}
		if created.ID != "doc_2" {
			t.Fatalf("reused source ID: %+v", created)
		}
		_, e = store.Change(ctx, "edit", created.ID, rag.ChunkChange{Version: 1, Content: "更新后的共享正文"}, key, lockToken)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	personalStore := rag.ChunkStore{Client: client, IndexKey: rag.BuildStoredFileIndexKey(files[1].StoredFileID)}
	shared, err := personalStore.Get(ctx, "doc_2")
	if err != nil || shared.Content != "更新后的共享正文" {
		t.Fatalf("personal reference not synchronized: %+v %v", shared, err)
	}
	if w := request("PATCH", "enterprise-chunks/chunks/doc_2", `{"enabled":false,"version":2}`); w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	// Removing one reference preserves disabled data and its high-water mark.
	if err := fileService.DeleteKnowledgeFile(model.EnterpriseKnowledgeOwner("chunk-admin"), "enterprise-chunks"); err != nil {
		t.Fatal(err)
	}
	if _, err := personalStore.Get(ctx, "doc_2"); err != nil {
		t.Fatalf("shared data removed too early: %v", err)
	}
	if err := fileService.DeleteKnowledgeFile(model.PersonalKnowledgeOwner("alice"), "personal-chunks"); err != nil {
		t.Fatal(err)
	}
	if client.Exists(ctx, rag.DisabledChunkPrefix(indexKey)+"doc_2", rag.ChunkMetaPrefix(indexKey)+"sequence").Val() != 0 {
		t.Fatal("orphaned chunk management data")
	}
}

package router

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmysql "GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils/myjwt"
	"github.com/gin-gonic/gin"
)

func TestKnowledgePermissionsAndLiveRole(t *testing.T) {
	db := testutil.MySQL(t)
	previous := appmysql.DB
	appmysql.DB = db
	t.Cleanup(func() { appmysql.DB = previous })
	if err := db.AutoMigrate(&model.User{}, &model.KnowledgeFile{}); err != nil {
		t.Fatal(err)
	}
	users := []model.User{{UID: "alice", Email: "alice@example.com", Role: model.RoleUser}, {UID: "bob", Email: "bob@example.com", Role: model.RoleUser}, {UID: "admin", Email: "admin@example.com", Role: model.RoleAdmin}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	files := []model.KnowledgeFile{
		{ID: "alice-file", UID: "alice", Scope: "personal", OwnerKey: "alice", StoredName: "alice.txt", OriginalName: "Alice.txt", StoredFileID: "a"},
		{ID: "bob-file", UID: "bob", Scope: "personal", OwnerKey: "bob", StoredName: "bob.txt", OriginalName: "Bob.txt", StoredFileID: "b"},
		{ID: "enterprise-file", UID: "admin", Scope: "enterprise", OwnerKey: model.EnterpriseOwnerKey, StoredName: "shared.txt", OriginalName: "Shared.txt", StoredFileID: "e"},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := InitRouter()
	tokens := map[string]string{}
	for _, user := range users {
		token, err := myjwt.GenerateToken(user.UID)
		if err != nil {
			t.Fatal(err)
		}
		tokens[user.UID] = token
	}
	request := func(uid, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v3"+path, strings.NewReader(body))
		if uid != "" {
			req.Header.Set("Authorization", "Bearer "+tokens[uid])
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, user := range []string{"alice", "bob"} {
		w := request(user, "GET", "/file/list?scope=enterprise&owner_key=bob", "")
		var result struct {
			Files []model.KnowledgeFile `json:"files"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if len(result.Files) != 1 || result.Files[0].ID != user+"-file" {
			t.Fatalf("personal isolation failed: %s", w.Body.String())
		}
		other := "bob"
		if user == "bob" {
			other = "alice"
		}
		w = request(user, "DELETE", "/file/"+other+"-file", "")
		if strings.Contains(w.Body.String(), `"status_code":1000`) {
			t.Fatal("deleted another user's personal file")
		}
	}
	for _, route := range r.Routes() {
		if !strings.HasPrefix(route.Path, "/api/v3/enterprise/file/") {
			continue
		}
		path := strings.ReplaceAll(strings.TrimPrefix(route.Path, "/api/v3"), ":id", "enterprise-file")
		path = strings.ReplaceAll(path, ":chunk_id", "doc_1")
		methodPath := [2]string{route.Method, path}
		w := request("alice", methodPath[0], methodPath[1], `{"role":"admin"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("enterprise access accepted: %s %s: %s", methodPath[0], methodPath[1], w.Body.String())
		}
		if anonymous := request("", methodPath[0], methodPath[1], `{}`); strings.Contains(anonymous.Body.String(), `"status_code":1000`) {
			t.Fatal("anonymous enterprise access accepted", methodPath)
		}
	}
	for _, path := range []string{"/enterprise/file/alice-file/chunks", "/enterprise/file/bob-file/chunks/doc_1"} {
		if w := request("admin", "GET", path, ""); w.Code != http.StatusNotFound {
			t.Fatalf("admin chunk endpoint exposed personal file: %s", w.Body.String())
		}
	}
	// Every ordinary-user business route must reject administrators before invoking handlers.
	for _, route := range r.Routes() {
		if route.Path != "/api/v3/memory" && !strings.HasPrefix(route.Path, "/api/v3/AI/") && !strings.HasPrefix(route.Path, "/api/v3/image/") && !strings.HasPrefix(route.Path, "/api/v3/file/") && !strings.HasPrefix(route.Path, "/api/v3/medical/") {
			continue
		}
		path := strings.ReplaceAll(strings.TrimPrefix(route.Path, "/api/v3"), ":id", "bob-file")
		if w := request("admin", route.Method, path, `{}`); w.Code != http.StatusForbidden {
			t.Fatalf("admin accessed %s %s: %s", route.Method, path, w.Body.String())
		}
	}
	w := request("admin", "GET", "/enterprise/file/list", "")
	if !strings.Contains(w.Body.String(), "enterprise-file") || strings.Contains(w.Body.String(), "bob-file") {
		t.Fatal(w.Body.String())
	}
	w = request("admin", "GET", "/user/me", "")
	if !strings.Contains(w.Body.String(), `"role":"admin"`) || strings.Contains(w.Body.String(), "password") {
		t.Fatal(w.Body.String())
	}
	w = request("admin", "POST", "/enterprise/file/precheck", `{}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status_code":2001`) {
		t.Fatal("administrator did not reach upload validation", w.Body.String())
	}
	// Same token must lose enterprise access immediately after role is changed in the DB.
	if err := db.Model(&model.User{}).Where("uid = ?", "admin").Update("role", model.RoleUser).Error; err != nil {
		t.Fatal(err)
	}
	if w := request("admin", "POST", "/enterprise/file/precheck", `{}`); w.Code != http.StatusForbidden {
		t.Fatal("stale token retained admin rights")
	}
	if w := request("admin", "GET", "/enterprise/file/list", ""); w.Code != http.StatusForbidden {
		t.Fatal("stale token retained enterprise read access")
	}
	if w := request("admin", "GET", "/file/list", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status_code":1000`) {
		t.Fatal("new ordinary-user role did not gain personal access", w.Body.String())
	}
	if w := request("", "GET", "/enterprise/file/list", ""); strings.Contains(w.Body.String(), `"status_code":1000`) {
		t.Fatal("anonymous read accepted")
	}
}

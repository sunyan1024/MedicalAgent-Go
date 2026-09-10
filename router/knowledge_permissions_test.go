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
	users := []model.User{{Username: "alice", Role: model.RoleUser}, {Username: "bob", Role: model.RoleUser}, {Username: "admin", Role: model.RoleAdmin}}
	if err := db.Create(&users).Error; err != nil {
		t.Fatal(err)
	}
	files := []model.KnowledgeFile{
		{ID: "alice-file", UserName: "alice", Scope: "personal", OwnerKey: "alice", StoredName: "alice.txt", OriginalName: "Alice.txt", StoredFileID: "a"},
		{ID: "bob-file", UserName: "bob", Scope: "personal", OwnerKey: "bob", StoredName: "bob.txt", OriginalName: "Bob.txt", StoredFileID: "b"},
		{ID: "enterprise-file", UserName: "admin", Scope: "enterprise", OwnerKey: model.EnterpriseOwnerKey, StoredName: "shared.txt", OriginalName: "Shared.txt", StoredFileID: "e"},
	}
	if err := db.Create(&files).Error; err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	r := InitRouter()
	tokens := map[string]string{}
	for _, user := range users {
		token, err := myjwt.GenerateToken(user.ID, user.Username)
		if err != nil {
			t.Fatal(err)
		}
		tokens[user.Username] = token
	}
	request := func(username, method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "/api/v2"+path, strings.NewReader(body))
		if username != "" {
			req.Header.Set("Authorization", "Bearer "+tokens[username])
		}
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, user := range []string{"alice", "admin"} {
		w := request(user, "GET", "/file/list?scope=enterprise&owner_key=bob", "")
		var result struct {
			Files []model.KnowledgeFile `json:"files"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		want := 0
		if user == "alice" {
			want = 1
		}
		if len(result.Files) != want || (want == 1 && result.Files[0].ID != "alice-file") {
			t.Fatalf("personal isolation failed: %s", w.Body.String())
		}
		w = request(user, "DELETE", "/file/bob-file", "")
		if strings.Contains(w.Body.String(), `"status_code":1000`) {
			t.Fatal("deleted another user's personal file")
		}
	}
	for _, methodPath := range [][2]string{{"POST", "/enterprise/file/precheck"}, {"POST", "/enterprise/file/upload"}, {"DELETE", "/enterprise/file/enterprise-file"}} {
		w := request("alice", methodPath[0], methodPath[1], `{"role":"admin"}`)
		if w.Code != http.StatusForbidden {
			t.Fatalf("write accepted: %s %s: %s", methodPath[0], methodPath[1], w.Body.String())
		}
	}
	w := request("alice", "GET", "/enterprise/file/list", "")
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
	// Same token must lose write access immediately after role is changed in the DB.
	if err := db.Model(&model.User{}).Where("username = ?", "admin").Update("role", model.RoleUser).Error; err != nil {
		t.Fatal(err)
	}
	if w := request("admin", "POST", "/enterprise/file/precheck", `{}`); w.Code != http.StatusForbidden {
		t.Fatal("stale token retained admin rights")
	}
	if w := request("", "GET", "/enterprise/file/list", ""); strings.Contains(w.Body.String(), `"status_code":1000`) {
		t.Fatal("anonymous read accepted")
	}
}

package router

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"GoAI/common/mysql"
	"GoAI/internal/testutil"
	"GoAI/model"
	"GoAI/utils/myjwt"
)

func TestMemoryAPIAuthIsolationAndConflict(t *testing.T) {
	db := testutil.MySQL(t)
	previous := mysql.DB
	mysql.DB = db
	t.Cleanup(func() { mysql.DB = previous })
	if err := db.AutoMigrate(&model.User{}, &model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	tokens := map[string]string{}
	for _, uid := range []string{"alice", "bob", "admin"} {
		role := model.RoleUser
		if uid == "admin" {
			role = model.RoleAdmin
		}
		if err := db.Create(&model.User{UID: uid, Role: role, Email: uid + "@example.com"}).Error; err != nil {
			t.Fatal(err)
		}
		token, err := myjwt.GenerateToken(uid)
		if err != nil {
			t.Fatal(err)
		}
		tokens[uid] = token
	}
	r := InitRouter()
	request := func(uid, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/api/v3/memory?uid=alice", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if uid != "" {
			req.Header.Set("Authorization", "Bearer "+tokens[uid])
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, method := range []string{"GET", "PUT"} {
		w := request("", method, `{}`)
		if strings.Contains(w.Body.String(), "contentMarkdown") || w.Body.String() == "{}" {
			t.Fatal("unauthenticated request reached handler", w.Body.String())
		}
		if w = request("admin", method, `{}`); w.Code != 403 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request("alice", "PUT", `{"contentMarkdown":"用户自述过敏","expectedVersion":0}`)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("bob", "GET", "")
	var doc model.UserMemory
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil || doc.Version != 0 || doc.ContentMarkdown != "" {
		t.Fatal(w.Body.String(), err)
	}
	if w = request("alice", "PUT", `{"contentMarkdown":"stale","expectedVersion":0}`); w.Code != 409 {
		t.Fatal(w.Code)
	}
	for _, body := range []string{`{}`, `{"contentMarkdown":"x","expectedVersion":1,"uid":"bob"}`, `{"contentMarkdown":"x","expectedVersion":1} {}`} {
		if w = request("alice", "PUT", body); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w = request("alice", "PUT", `{"contentMarkdown":"","expectedVersion":1}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = request("alice", "GET", "")
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil || doc.Version != 2 || doc.ContentMarkdown != "" {
		t.Fatal(w.Body.String(), err)
	}
}

package jwt

import (
	"GoAI/model"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRoleGuards(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, guard := range []struct {
		name       string
		role       string
		middleware gin.HandlerFunc
	}{{"admin", model.RoleAdmin, RequireAdmin()}, {"user", model.RoleUser, RequireUser()}} {
		for _, account := range []struct {
			name  string
			value interface{}
			role  string
		}{
			{"admin", &model.User{Role: model.RoleAdmin}, model.RoleAdmin},
			{"user", &model.User{Role: model.RoleUser}, model.RoleUser},
			{"unknown", &model.User{Role: "unknown"}, "unknown"},
			{"missing", nil, ""},
			{"nil-user", (*model.User)(nil), ""},
			{"wrong-type", "admin", ""},
		} {
			t.Run(guard.name+"/"+account.name, func(t *testing.T) {
				r := gin.New()
				r.Use(func(c *gin.Context) {
					if account.value != nil {
						c.Set("currentUser", account.value)
					}
					c.Next()
				})
				r.GET("/", guard.middleware, func(c *gin.Context) { c.Status(http.StatusNoContent) })
				w := httptest.NewRecorder()
				r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))
				want := http.StatusForbidden
				if account.role == guard.role {
					want = http.StatusNoContent
				}
				if w.Code != want {
					t.Fatalf("status = %d, want %d", w.Code, want)
				}
			})
		}
	}
}

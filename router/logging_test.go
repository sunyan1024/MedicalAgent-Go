package router

import (
	"GoAI/common/logging"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDailyRequestAndRecoveryLogs(t *testing.T) {
	oldOut, oldErr := gin.DefaultWriter, gin.DefaultErrorWriter
	t.Cleanup(func() { gin.DefaultWriter, gin.DefaultErrorWriter = oldOut, oldErr })
	dir := t.TempDir()
	closeLogs, err := logging.InitDailyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLogs() })
	r := InitRouter()
	r.GET("/test-log-ok", func(c *gin.Context) { c.Status(204) })
	r.GET("/test-log-panic", func(c *gin.Context) { panic("daily-recovery-marker") })
	for path, status := range map[string]int{"/test-log-ok": 204, "/test-log-panic": 500} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != status {
			t.Fatalf("%s: %d", path, w.Code)
		}
	}
	if err := closeLogs(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	b, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	if strings.Contains(s, "\x1b[") {
		t.Fatal("ANSI color in log")
	}
	for _, path := range []string{"/test-log-ok", "/test-log-panic"} {
		count := 0
		for _, line := range strings.Split(s, "\n") {
			if strings.Contains(line, "[GIN]") && strings.Contains(line, path) {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("request %s logged %d times", path, count)
		}
	}
	if !strings.Contains(s, "[Recovery]") || !strings.Contains(s, "daily-recovery-marker") {
		t.Fatal("missing recovery log")
	}
}

package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDailyRotationAndAppend(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 19, 23, 59, 59, 0, time.Local)
	w := &dailyWriter{dir: dir, now: func() time.Time { return now }}
	if _, err := w.Write([]byte("first\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}
	w.file = nil
	if _, err := w.Write([]byte("restart\n")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	if _, err := w.Write([]byte("next\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.file.Close(); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, filepath.Join(dir, "2026-09-19.log")); got != "first\nrestart\n" {
		t.Fatalf("previous day: %q", got)
	}
	if got := readLog(t, filepath.Join(dir, "2026-09-20.log")); got != "next\n" {
		t.Fatalf("next day: %q", got)
	}
}

func TestDualOutputConcurrentAndPlainText(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	closeLogs, err := InitDailyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeLogs() })
	var console bytes.Buffer
	writer := NewColoredTimeWriter(&console)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, err := fmt.Fprintf(writer, "record-%03d\n", i); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	// GORM uses this constructor as its logger; it must share the same sink.
	NewColoredLogger().Print("gorm-record")
	if err := closeLogs(); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.log"))
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	got := readLog(t, files[0])
	if strings.Contains(got, "\x1b[") {
		t.Fatal("ANSI colors in file")
	}
	if !strings.Contains(console.String(), "\x1b[") {
		t.Fatal("console lost colors")
	}
	for i := 0; i < 100; i++ {
		record := fmt.Sprintf("record-%03d\n", i)
		if strings.Count(got, record) != 1 || strings.Count(console.String(), record) != 1 {
			t.Fatalf("missing or duplicate %s", record)
		}
	}
	if strings.Count(got, "gorm-record") != 1 {
		t.Fatal("GORM logger not routed")
	}
}

func TestFailureAndRetry(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.WriteFile(blocked, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := InitDailyFile(filepath.Join(blocked, "logs")); err == nil {
		t.Fatal("expected initialization error")
	}
	dir := filepath.Join(root, "logs")
	closeLogs, err := InitDailyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogs()
	var console bytes.Buffer
	writer := NewOutputWriter(&console)
	// Simulate a failed descriptor, then a failed reopen, then recovery.
	fileOutput.daily.file.Close()
	if _, err := writer.Write([]byte("closed\n")); err == nil {
		t.Fatal("expected write error")
	}
	fileOutput.daily.dir = filepath.Join(blocked, "logs")
	if _, err := writer.Write([]byte("blocked\n")); err == nil {
		t.Fatal("expected open error")
	}
	fileOutput.daily.dir = dir
	if _, err := writer.Write([]byte("recovered\n")); err != nil {
		t.Fatal(err)
	}
	if console.String() != "closed\nblocked\nrecovered\n" {
		t.Fatal("console output lost")
	}
	if got := readLog(t, filepath.Join(dir, fileOutput.daily.date+".log")); got != "recovered\n" {
		t.Fatalf("recovery: %q", got)
	}
}

type failingConsole struct{}

func (failingConsole) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestConsoleFailureStillWritesFile(t *testing.T) {
	dir := t.TempDir()
	closeLogs, err := InitDailyFile(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closeLogs()
	if _, err := NewOutputWriter(failingConsole{}).Write([]byte("saved\n")); err == nil {
		t.Fatal("expected console error")
	}
	if got := readLog(t, filepath.Join(dir, fileOutput.daily.date+".log")); got != "saved\n" {
		t.Fatalf("file: %q", got)
	}
}

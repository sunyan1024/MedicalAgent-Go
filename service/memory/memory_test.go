package memory

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"GoAI/internal/testutil"
	"GoAI/model"
)

func TestMemoryPersistenceIsolationAndCAS(t *testing.T) {
	db := testutil.MySQL(t)
	if err := db.AutoMigrate(&model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	s := New(db)
	doc, err := s.Read(ctx, "alice")
	if err != nil || doc.Version != 0 || doc.ContentMarkdown != "" {
		t.Fatal(doc, err)
	}
	content := "# 回答偏好\n简洁\n# 本人健康背景\n用户自述青霉素过敏\n# 家属情况\n父亲有高血压"
	doc, err = s.Update(ctx, "alice", content, 0)
	if err != nil || doc.Version != 1 {
		t.Fatal(doc, err)
	}
	if _, err = s.Update(ctx, "alice", "duplicate", 0); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	// A new store has no process-local memory; the database restores the document.
	doc, err = New(db).Read(ctx, "alice")
	if err != nil || doc.ContentMarkdown != content {
		t.Fatal(doc, err)
	}
	other, err := s.Read(ctx, "bob")
	if err != nil || other.Version != 0 || other.ContentMarkdown != "" {
		t.Fatal(other, err)
	}
	if _, err = s.Update(ctx, "bob", "overwrite alice", 1); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, text := range []string{"new A", "new B"} {
		wg.Add(1)
		go func(text string) { defer wg.Done(); _, e := New(db).Update(ctx, "alice", text, 1); results <- e }(text)
	}
	wg.Wait()
	close(results)
	ok, conflict := 0, 0
	for e := range results {
		if e == nil {
			ok++
		} else if errors.Is(e, ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
	doc, err = s.Update(ctx, "alice", "", 2)
	if err != nil || doc.Version != 3 || doc.ContentMarkdown != "" {
		t.Fatal(doc, err)
	}
	if _, err = s.Update(ctx, "alice", "restore stale", 2); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "alice", strings.Repeat("医", 8000), 3); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "alice", strings.Repeat("医", 8001), 4); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "alice", "x", -1); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "", "x", 0); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err = db.Migrator().DropTable(&model.UserMemory{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Read(ctx, "alice"); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, "alice", "private body", 4); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

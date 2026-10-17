package medical

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSearchWithTimeout(t *testing.T) {
	start := time.Now()
	wantErr := errors.New("retrieval failed")
	var searchCtx context.Context
	_, err := searchWithTimeout(context.Background(), "医学资料", func(ctx context.Context, query string) ([]Source, error) {
		searchCtx = ctx
		deadline, ok := ctx.Deadline()
		if !ok || deadline.Before(start.Add(59*time.Second)) || deadline.After(time.Now().Add(60*time.Second)) {
			t.Fatalf("unexpected retrieval deadline: %v, present=%v", deadline, ok)
		}
		if query != "医学资料" {
			t.Fatalf("query changed: %q", query)
		}
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("retrieval error lost: %v", err)
	}
	if searchCtx.Err() != context.Canceled {
		t.Fatalf("retrieval context not released: %v", searchCtx.Err())
	}
}

func TestSearchWithTimeoutHonorsParent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	_, err := searchWithTimeout(ctx, "医学资料", func(searchCtx context.Context, _ string) ([]Source, error) {
		if deadline, _ := searchCtx.Deadline(); !deadline.Equal(wantDeadline) {
			t.Fatalf("parent deadline changed: %v", deadline)
		}
		cancel()
		<-searchCtx.Done()
		return nil, searchCtx.Err()
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation lost: %v", err)
	}
}

func TestSearchWithTimeoutRejectsInvalidInputBeforeRetrieval(t *testing.T) {
	for _, query := range []string{"", " \n", strings.Repeat("a", 12001)} {
		_, err := searchWithTimeout(context.Background(), query, func(context.Context, string) ([]Source, error) {
			t.Fatal("invalid query reached retrieval")
			return nil, nil
		})
		if err == nil {
			t.Fatal("invalid query accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := searchWithTimeout(ctx, "医学资料", func(context.Context, string) ([]Source, error) {
		t.Fatal("canceled request reached retrieval")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled request accepted: %v", err)
	}
}

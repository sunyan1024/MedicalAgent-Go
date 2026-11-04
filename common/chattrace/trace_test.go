package chattrace

import (
	"context"
	"testing"
)

func TestFirstMilestoneAndIsolation(t *testing.T) {
	ctx, trace := Start(context.Background())
	Mark(ctx, "first_body")
	trace.stages["first_body"] = 123
	Mark(ctx, "first_body")
	if trace.stages["first_body"] != 123 {
		t.Fatal("first milestone overwritten")
	}
	if _, ok := trace.stages["first_reasoning"]; ok {
		t.Fatal("missing stage must remain absent")
	}
	_, other := Start(context.Background())
	if trace.ID == other.ID || len(other.stages) != 0 {
		t.Fatal("requests share timing state")
	}
	Mark(context.Background(), "ignored")
}

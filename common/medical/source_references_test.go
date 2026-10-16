package medical

import "testing"

func TestSourceReferencesMappingAndIsolation(t *testing.T) {
	// The real source whose hash was copied incorrectly in the failed run.
	web := Source{ID: "web_be34e68507ec8b94feaf897da738b11be288aadfbdd94b0c6444373e11df59fe", Kind: "web", URL: "https://example.org", Title: "指南", Content: "证据"}
	knowledge := Source{ID: "knowledge_document_1", Kind: "knowledge", Content: "本地证据"}
	r := newSourceReferences()
	input := []Source{knowledge, web}
	got := r.register(input)
	if got[0].ID != "S1" || got[1].ID != "S2" || input[1] != web {
		t.Fatalf("unexpected aliases or mutated input: %+v", got)
	}
	if again := r.register([]Source{web}); again[0].ID != "S2" {
		t.Fatalf("duplicate source changed alias: %+v", again)
	}
	read, ok := r.lookup("S2")
	want := web
	want.ID = "S2"
	if !ok || read != want {
		t.Fatalf("read source: %+v, %v", read, ok)
	}
	ids, err := r.resolve([]string{"S2", "S1"})
	if err != nil || len(ids) != 2 || ids[0] != web.ID || ids[1] != knowledge.ID {
		t.Fatalf("resolved IDs: %v, %v", ids, err)
	}
	if ids, err := r.resolve([]string{}); err != nil || ids == nil || len(ids) != 0 {
		t.Fatalf("empty evidence: %v, %v", ids, err)
	}
	for _, invalid := range []string{"S3", web.ID, "web_be34e6857ec8b94feaf897da738b11be288aadfbdd94b0c644373e11df59fe"} {
		if _, err := r.resolve([]string{"S1", invalid}); err == nil {
			t.Fatalf("accepted invalid reference %q", invalid)
		}
		if _, ok := r.lookup(invalid); ok {
			t.Fatalf("read invalid reference %q", invalid)
		}
	}
	other := newSourceReferences()
	other.register([]Source{knowledge})
	if _, err := other.resolve([]string{"S2"}); err == nil {
		t.Fatal("reference leaked between delegations")
	}
}

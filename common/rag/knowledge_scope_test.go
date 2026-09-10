package rag

import (
	"GoAI/model"
	"testing"
)

func TestUniqueKnowledgeFilesPrefersPersonalName(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		files := []model.KnowledgeFile{
			{StoredFileID: "shared", Scope: model.KnowledgeScopeEnterprise, OriginalName: "Enterprise.txt"},
			{StoredFileID: "shared", Scope: model.KnowledgeScopePersonal, OriginalName: "Personal.txt"},
			{StoredFileID: "enterprise-only", Scope: model.KnowledgeScopeEnterprise},
			{StoredFileID: ""},
		}
		if reverse {
			files[0], files[1] = files[1], files[0]
		}
		got := uniqueKnowledgeFiles(files)
		if len(got) != 2 || got[0].OriginalName != "Personal.txt" || got[1].StoredFileID != "enterprise-only" {
			t.Fatalf("unexpected targets: %+v", got)
		}
	}
	if got := uniqueKnowledgeFiles(nil); len(got) != 0 {
		t.Fatal(got)
	}
}

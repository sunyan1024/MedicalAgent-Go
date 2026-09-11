package rag

import (
	"GoAI/model"
	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
	"strings"
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

func TestMedicalRecordScopeSurvivesRetrievalAndPrompt(t *testing.T) {
	for _, tc := range []struct{ scope, label string }{
		{model.KnowledgeScopePersonal, "当前用户个人库"},
		{model.KnowledgeScopeEnterprise, "企业库"},
		{"", "来源未知"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			keyword := parseKeywordSearchResult([]interface{}{int64(1), "record", []interface{}{"content", "合成病历：患者甲，检查编号 A001。"}}, keywordSearchTarget{filename: "record.txt", scope: tc.scope})
			vector := []*schema.Document{convertKnowledgeVectorDocument(redis.Document{ID: "record", Fields: map[string]string{"content": keyword[0].Content, "knowledge_scope": "untrusted-index-value"}}, model.KnowledgeFile{Scope: tc.scope, StoredName: "record.txt"})}
			for _, docs := range [][]*schema.Document{keyword, mergeRRFDocuments(vector, keyword, 60)} {
				ranked := applyRerankResults(docs, []dashScopeRerankResult{{Index: 0, RelevanceScore: 0.9}})
				if len(ranked) != 1 || ranked[0].MetaData["knowledge_scope"] != tc.scope {
					t.Fatalf("scope lost: %+v", ranked)
				}
				prompt := BuildRAGPrompt("解读我的病历", ranked)
				if !strings.Contains(prompt, "资料来源："+tc.label+"\n") || !strings.Contains(prompt, "检查编号 A001") {
					t.Fatalf("missing provenance or content: %s", prompt)
				}
			}
		})
	}
}

func TestMedicalRecordPromptOwnershipRules(t *testing.T) {
	prompt := BuildRAGPrompt("这是我父亲的病历", []*schema.Document{{Content: "合成病历", MetaData: map[string]any{"knowledge_scope": model.KnowledgeScopePersonal}}})
	for _, want := range []string{"默认属于当前用户本人", "以用户最新的明确说明为准", "企业库病例仅作为参考", "来源未知的病历不推定属于本人", "先询问用户", "未检索到相关病历时", "用户原始问题：这是我父亲的病历"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing rule %q", want)
		}
	}
}

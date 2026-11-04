package utils

import (
	"GoAI/model"
	"github.com/cloudwego/eino/schema"
	"testing"
)

func TestMessageRoleRoundTrip(t *testing.T) {
	for _, role := range []schema.RoleType{schema.System, schema.User, schema.Assistant, schema.Tool} {
		t.Run(string(role), func(t *testing.T) {
			stored := ConvertToModelMessage("session", "user", &schema.Message{Role: role, Content: "content"})
			restored := ConvertToSchemaMessages([]*model.Message{stored})
			if stored.Role != string(role) || restored[0].Role != role || restored[0].Content != "content" {
				t.Fatalf("role round trip failed: %+v", stored)
			}
		})
	}
}

package medical

import "testing"

func TestCleanAnswerSourceMarkers(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"web", "结论[web_abc-123]。", "结论。"},
		{"knowledge", "结论[knowledge_abc_1]。", "结论。"},
		{"prefix", "结论[来源ID: web_abc]。", "结论。"},
		{"spaces", "结论[ 来源 ID ： knowledge_abc_1 ]。", "结论。"},
		{"repeated unknown", "[web_unknown]结论[web_unknown][knowledge_missing]", "结论"},
		{"plain", "第一项[1]，指标 3.5。\n\n第二段。", "第一项[1]，指标 3.5。\n\n第二段。"},
		{"links", "[资料](https://example.org) [web_abc](https://example.org)", "[资料](https://example.org) [web_abc](https://example.org)"},
		{"reference", "[web_abc][ref]\n[web_abc]: https://example.org", "[web_abc][ref]\n[web_abc]: https://example.org"},
		{"mixed", "[web_a]正文[web_b](https://example.org)[knowledge_c]", "正文[web_b](https://example.org)"},
		{"only markers", "[web_a][knowledge_b]", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := cleanAnswerSourceMarkers(tc.input); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

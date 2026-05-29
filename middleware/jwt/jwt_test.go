package jwt

import "testing"

func TestMaskToken(t *testing.T) {
	tests := []struct {
		name  string
		token string
		want  string
	}{
		{
			name:  "long token",
			token: "1234567890abcdefghijklmnopqrstuvwxyz0987654321",
			want:  "1234567890...0987654321",
		},
		{
			name:  "twenty chars token",
			token: "1234567890abcdefghij",
			want:  "<redacted>",
		},
		{
			name:  "short token",
			token: "short-token",
			want:  "<redacted>",
		},
		{
			name:  "empty token",
			token: "",
			want:  "<redacted>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := maskToken(tt.token)
			if got != tt.want {
				t.Fatalf("maskToken() = %q, want %q", got, tt.want)
			}
			if tt.want != tt.token {
				return
			}
			t.Fatalf("maskToken() returned the original token")
		})
	}
}

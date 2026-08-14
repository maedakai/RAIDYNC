package protocol

import "testing"

func TestNormalizeGameSessionID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "full ARN → gsess short ID",
			input: "arn:aws:gamelift:ap-northeast-1::gamesession/fleet-7056f254-1234-5678-abcd-000000000000/gsess-abcd1234-5678-90ef-1234-567890abcdef",
			want:  "gsess-abcd1234-5678-90ef-1234-567890abcdef",
		},
		{
			name:  "local short ID unchanged",
			input: "sess-550e8400-e29b-41d4-a716-446655440000",
			want:  "sess-550e8400-e29b-41d4-a716-446655440000",
		},
		{
			name:  "already normalized gsess",
			input: "gsess-abcd1234-5678-90ef-1234-567890abcdef",
			want:  "gsess-abcd1234-5678-90ef-1234-567890abcdef",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeGameSessionID(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeGameSessionID(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

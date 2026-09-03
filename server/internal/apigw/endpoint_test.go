package apigw

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestRegionEndpoint(t *testing.T) {
	req := func(domain, stage string) events.APIGatewayProxyRequest {
		return events.APIGatewayProxyRequest{
			RequestContext: events.APIGatewayProxyRequestContext{
				DomainName: domain,
				Stage:      stage,
			},
		}
	}

	tests := []struct {
		name    string
		envURL  string
		request events.APIGatewayProxyRequest
		want    string
	}{
		{
			name:    "execute-api ドメインから導出（リージョン固有）",
			request: req("0eyvmklmfb.execute-api.ap-northeast-1.amazonaws.com", "dev"),
			want:    "https://0eyvmklmfb.execute-api.ap-northeast-1.amazonaws.com/dev",
		},
		{
			name:    "セカンダリリージョンでも同様に導出",
			request: req("zpytz502zd.execute-api.us-east-1.amazonaws.com", "dev"),
			want:    "https://zpytz502zd.execute-api.us-east-1.amazonaws.com/dev",
		},
		{
			name:    "stage 無しならパスを付けない",
			request: req("example.execute-api.us-east-1.amazonaws.com", ""),
			want:    "https://example.execute-api.us-east-1.amazonaws.com",
		},
		{
			// localapi は RequestContext を埋めないため空になり、
			// クライアントは従来どおり自分の URL を使い続ける（ローカル挙動は不変）。
			name:    "ドメイン不明なら空",
			request: req("", "dev"),
			want:    "",
		},
		{
			// Route 53 導入後はリージョン固有のカスタムドメインを注入する。
			name:    "API_BASE_URL が最優先",
			envURL:  "https://apne1.api.example.com/dev",
			request: req("api.example.com", "dev"),
			want:    "https://apne1.api.example.com/dev",
		},
		{
			name:    "API_BASE_URL の末尾スラッシュは落とす",
			envURL:  "https://apne1.api.example.com/dev/",
			request: req("api.example.com", "dev"),
			want:    "https://apne1.api.example.com/dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("API_BASE_URL", tt.envURL)
			if got := RegionEndpoint(tt.request); got != tt.want {
				t.Errorf("RegionEndpoint() = %q, want %q", got, tt.want)
			}
		})
	}
}

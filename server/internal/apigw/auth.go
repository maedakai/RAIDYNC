package apigw

import (
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// PlayerID はリクエストから playerId を取り出す。
// Step 7 で mock-auth の JWT 検証がオーソライザ claims（sub）を埋める。
// それまでは X-Player-Id ヘッダで代用できるようフォールバックする。
func PlayerID(req events.APIGatewayProxyRequest) (string, bool) {
	if auth := req.RequestContext.Authorizer; auth != nil {
		if claims, ok := auth["claims"].(map[string]any); ok {
			if sub, ok := claims["sub"].(string); ok && sub != "" {
				return sub, true
			}
		}
		if sub, ok := auth["sub"].(string); ok && sub != "" {
			return sub, true
		}
	}
	if v := Header(req, "X-Player-Id"); v != "" {
		return v, true
	}
	return "", false
}

// Header はヘッダ値を大文字小文字を区別せず取得する。
func Header(req events.APIGatewayProxyRequest, key string) string {
	for k, v := range req.Headers {
		if strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}

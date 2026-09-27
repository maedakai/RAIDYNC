// Package apigw は API Gateway プロキシ統合向けの共通ヘルパー（応答整形・認証情報抽出）を提供する。
package apigw

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/raidync/server/internal/metrics"
)

var jsonHeaders = map[string]string{"Content-Type": "application/json"}

// JSON は body を JSON 化して指定ステータスで返す。
func JSON(ctx context.Context, status int, body any) events.APIGatewayProxyResponse {
	b, err := json.Marshal(body)
	if err != nil {
		return Error(ctx, http.StatusInternalServerError, "failed to encode response", err)
	}
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers:    jsonHeaders,
		Body:       string(b),
	}
}

// Error は {"error": message} 形式のエラー応答を返す。
func Error(ctx context.Context, status int, message string, err error) events.APIGatewayProxyResponse {
	b, _ := json.Marshal(map[string]string{"error": message})
	if err != nil && status >= 500 {
		metrics.EmitError(ctx, status, message, err)
	}

	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers:    jsonHeaders,
		Body:       string(b),
	}
}

func JWTError(ctx context.Context, message string, err error) {
	metrics.EmitError(ctx, 500, message, err)
}

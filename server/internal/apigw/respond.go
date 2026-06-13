// Package apigw は API Gateway プロキシ統合向けの共通ヘルパー（応答整形・認証情報抽出）を提供する。
package apigw

import (
	"encoding/json"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
)

var jsonHeaders = map[string]string{"Content-Type": "application/json"}

// JSON は body を JSON 化して指定ステータスで返す。
func JSON(status int, body any) events.APIGatewayProxyResponse {
	b, err := json.Marshal(body)
	if err != nil {
		return Error(http.StatusInternalServerError, "failed to encode response")
	}
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers:    jsonHeaders,
		Body:       string(b),
	}
}

// Error は {"error": message} 形式のエラー応答を返す。
func Error(status int, message string) events.APIGatewayProxyResponse {
	b, _ := json.Marshal(map[string]string{"error": message})
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers:    jsonHeaders,
		Body:       string(b),
	}
}

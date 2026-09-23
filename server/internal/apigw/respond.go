// Package apigw は API Gateway プロキシ統合向けの共通ヘルパー（応答整形・認証情報抽出）を提供する。
package apigw

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambdacontext"
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
		emitError(ctx, status, message, err)
	}

	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers:    jsonHeaders,
		Body:       string(b),
	}
}

func JWTError(ctx context.Context, message string, err error) {
	emitError(ctx, 500, message, err)
}

func emitError(ctx context.Context, status int, message string, err error) {
	reqID := ""
	if lc, ok := lambdacontext.FromContext(ctx); ok {
		reqID = lc.AwsRequestID
	}
	functionName := getenv("AWS_LAMBDA_FUNCTION_NAME", "localapi")

	// message をディメンション値に正規化する
	errorKind := errorKindOf(message, "unknown")

	metrics := []emfMetric{{Name: "HandlerError", Unit: "Count"}}
	dimensions := [][]string{{"FunctionName"}, {"FunctionName", "ErrorKind"}}
	directives := []emfDirective{{Namespace: "RAIDYNC/Handlers", Dimensions: dimensions, Metrics: metrics}}
	metadata := emfMetadata{Timestamp: time.Now().UnixMilli(), CloudWatchMetrics: directives}

	slog.ErrorContext(ctx, message,
		"status", status,
		"requestId", reqID,
		"err", err,
		"_aws", metadata,
		"FunctionName", functionName,
		"ErrorKind", errorKind,
		"HandlerError", 1)
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// errorKindOf は message を ErrorKind ディメンションの値に正規化する。
// message が空のときにディメンション値を空にするとその行はメトリクス化されないため、
// fallback を返して「ログは必ず出す・メトリクスも成立させる」を両立させる。
func errorKindOf(message, fallback string) string {
	if message != "" {
		return strings.ToLower(strings.ReplaceAll(message, " ", "_"))
	}
	return fallback
}

// EMF（Embedded Metric Format）の _aws ブロックを組み立てるための構造体群。
// フィールド名は EMF 仕様どおりの PascalCase にしてあり、encoding/json がそのまま
// キー名として出力する（json タグ不要）。小文字にすると出力されないので注意。
type emfMetric struct {
	Name string
	Unit string
}

type emfDirective struct {
	Namespace  string
	Dimensions [][]string
	Metrics    []emfMetric
}

type emfMetadata struct {
	Timestamp         int64
	CloudWatchMetrics []emfDirective
}

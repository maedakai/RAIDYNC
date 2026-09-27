// Package metrics は CloudWatch のカスタムメトリクスを EMF（Embedded Metric Format）で
// 出力する。やることは「_aws ブロックを含む JSON を slog に 1 行書く」だけで、AWS SDK も
// HTTP も使わない。メトリクスへの変換は CloudWatch Logs が取り込み時に行うため、
// cloudwatch:PutMetricData 権限も不要。
//
// 依存を持たない最下層のパッケージとして保つこと（HTTP やドメインロジックを持ち込まない）。
// apigw / matchmaking など上位のパッケージが、用途別の関数を通してここを呼ぶ。
//
// 注意: _aws.Timestamp を落とすとログは正常に出たままメトリクスだけが静かに作られない。
// スキーマの検証は emitter_test.go が担保している。
package metrics

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/lambdacontext"
)

func EmitError(ctx context.Context, status int, message string, err error) {
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

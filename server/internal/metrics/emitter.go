// Package metrics は CloudWatch のカスタムメトリクスを EMF（Embedded Metric Format）で
// 出力する。やることは「_aws ブロックを含む JSON を slog に 1 行書く」だけで、AWS SDK も
// HTTP も使わない。メトリクスへの変換は CloudWatch Logs が取り込み時に行うため、
// cloudwatch:PutMetricData 権限も不要。
//
// 依存を持たない最下層のパッケージとして保つこと（HTTP やドメインロジックを持ち込まない）。
// apigw / matchmaking など上位のパッケージが、用途別の関数を通してここを呼ぶ。
//
// 注意 1: _aws.Timestamp を落とすとログは正常に出たままメトリクスだけが静かに作られない。
// スキーマの検証は emitter_test.go が担保している。
//
// 注意 2: EMF はログに乗って運ばれるため、**ログレベルのフィルタがメトリクスの存在に直結する**。
// マッチング系（MatchFormed / MatchTimeout）は INFO で出しているので、main の
// slog.SetDefault に &slog.HandlerOptions{Level: slog.LevelWarn} のような設定を入れると、
// その行自体が出力されなくなりメトリクスが静かに消える（エラー系だけが残る）。
// ログ量を絞りたくなったときは、レベルではなく出力箇所を減らすこと。
package metrics

import (
	"context"
	"log/slog"
	"os"
	"strconv"
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
	emit(ctx, slog.LevelError, message, directives,
		"status", status,
		"requestId", reqID,
		"err", err,
		"FunctionName", functionName,
		"ErrorKind", errorKind,
		"HandlerError", 1)
}

func MatchFormed(ctx context.Context, questID string, partySize int, waitMs int64) {
	partySizeStr := strconv.Itoa(partySize)

	metrics := []emfMetric{{Name: "MatchFormed", Unit: "Count"}, {Name: "MatchWaitTime", Unit: "Milliseconds"}}
	dimensions := [][]string{{"PartySize"}, {"QuestID", "PartySize"}}
	directives := []emfDirective{{Namespace: "RAIDYNC/Matchmaking", Dimensions: dimensions, Metrics: metrics}}
	emit(ctx, slog.LevelInfo, "match formed", directives,
		"MatchFormed", 1,
		"MatchWaitTime", waitMs,
		"QuestID", questID,
		"PartySize", partySizeStr)
}

func MatchTimeout(ctx context.Context, questID string) {
	metrics := []emfMetric{{Name: "MatchTimeout", Unit: "Count"}}
	dimensions := [][]string{{"QuestID"}}
	directives := []emfDirective{{Namespace: "RAIDYNC/Matchmaking", Dimensions: dimensions, Metrics: metrics}}
	emit(ctx, slog.LevelInfo, "match timeout", directives,
		"QuestID", questID,
		"MatchTimeout", 1)
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

func emit(ctx context.Context, level slog.Level, msg string, directives []emfDirective, attrs ...any) {

	metadata := emfMetadata{Timestamp: time.Now().UnixMilli(), CloudWatchMetrics: directives}
	args := append([]any{"_aws", metadata}, attrs...)
	slog.Log(ctx, level, msg, args...)
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

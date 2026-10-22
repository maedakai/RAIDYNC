// Package telemetry は OpenTelemetry のトレースを初期化する。
// 送り先は標準の環境変数 OTEL_EXPORTER_OTLP_ENDPOINT で決まり、未設定なら localhost:4317（OTLP gRPC）。
// ローカルでは docker-compose の Jaeger、AWS では Collector が受け取る想定。
package telemetry

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-lambda-go/lambda"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// newProvider は OTLP gRPC の Exporter で TracerProvider を作り、グローバルに登録する。
// Init（常駐プロセス用）と StartLambda（Lambda 用）の共通部分で、違いは後片付けの仕方だけ。
func newProvider(ctx context.Context, serviceName string) (*sdktrace.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res := resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(serviceName),
	)

	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(exporter))
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp, nil
}

// Init は OTel を初期化し、終了時に呼ぶ後片付け関数を返す。
// 後片付け関数はバッファに残った Span を送り切ってから閉じる（呼ばないと終了直前の Span が欠ける）。
// 送り先に届かなくても Init は失敗しない（接続は送信時に試みられ、失敗した Span は捨てられる）。
func Init(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	tp, err := newProvider(ctx, serviceName)
	if err != nil {
		return nil, fmt.Errorf("initialize telemetry: %w", err)
	}
	return tp.Shutdown, nil
}

// StartLambda は OTel を初期化し、ハンドラーを計測で包んで lambda.Start する（戻らない）。
// Lambda はレスポンスを返すと凍結され、終了時の後片付けが来ないため、Init のように Shutdown を
// 使えない。代わりに WithFlusher で呼び出しのたびに ForceFlush し、凍結前に Span を送り切る。
// handler は lambda.Start と同じく任意の形を受け取る（authorizer も他の関数も渡せる）。
func StartLambda(serviceName string, handler any) {
	tp, err := newProvider(context.Background(), serviceName)
	if err != nil {
		log.Fatalf("init telemetry: %v", err)
	}
	lambda.Start(otellambda.InstrumentHandler(handler, otellambda.WithFlusher(tp)))
}

// Package telemetry は OpenTelemetry のトレースを初期化する。
// 送り先は環境変数 TRACE_EXPORTER で切り替える（newExporter を参照）。
// ローカルは docker-compose の Jaeger（OTLP gRPC）、AWS の Lambda は X-Ray の OTLP エンドポイント（HTTP）に直接送る。
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/config"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-lambda-go/otellambda"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
)

// newProvider は newExporter が選んだ Exporter で TracerProvider を作り、グローバルに登録する。
// Init（常駐プロセス用）と StartLambda（Lambda 用）の共通部分で、違いは後片付けの仕方だけ。
func newProvider(ctx context.Context, serviceName string) (*sdktrace.TracerProvider, error) {
	exporter, err := newExporter(ctx)
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

// newExporter は TRACE_EXPORTER に応じて Span の送り先を選ぶ。どちらを選んだかは起動時にログへ出す。
//   - "xray": X-Ray の OTLP エンドポイントへ HTTP で送る（Lambda 用。Terraform で設定する）。
//     X-Ray は gRPC を受け付けないため HTTP を使う。region は Lambda が自動設定する AWS_REGION から取る。
//     リクエストには sigv4RoundTripper で SigV4 署名を付ける（exporter_xray.go）。
//   - それ以外（未設定）: OTLP gRPC で送る（localapi 用）。送り先は標準の OTEL_EXPORTER_OTLP_ENDPOINT
//     で決まり、未設定なら localhost:4317。
//
// 渡し忘れると AWS でも gRPC 側になり Span が届かないため、ログで気づけるようにしている。
func newExporter(ctx context.Context) (sdktrace.SpanExporter, error) {
	if os.Getenv("TRACE_EXPORTER") == "xray" {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			return nil, errors.New("AWS_REGION is not set")
		}
		endpoint := "https://xray." + region + ".amazonaws.com/v1/traces"
		log.Printf("TRACE_EXPORTER=xray: X-Ray Exporter を使用 (endpoint=%s)", endpoint)

		// 使うのは認証情報だけ。X-Ray は本物の AWS にしか送らないため、LocalStack 用の設定は要らない。
		cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("load aws config: %w", err)
		}
		client := &http.Client{Transport: &sigv4RoundTripper{
			next:   http.DefaultTransport,
			signer: v4.NewSigner(),
			creds:  cfg.Credentials,
			region: region,
		}}
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(endpoint),
			otlptracehttp.WithHTTPClient(client),
		)
	}

	log.Printf("TRACE_EXPORTER 未設定または xray 以外: OTLP gRPC Exporter を使用 (OTEL_EXPORTER_OTLP_ENDPOINT=%q)", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	return otlptracegrpc.New(ctx)
}

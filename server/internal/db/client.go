// Package db は DynamoDB への CRUD アクセスをテーブル単位で提供する。
// 上位層（matchmaking・lambda）は本パッケージ経由でのみ DynamoDB に触れる。
package db

import (
	"context"
	"os"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"go.opentelemetry.io/contrib/instrumentation/github.com/aws/aws-sdk-go-v2/otelaws"
)

// Tables は各論理テーブルの実名を保持する（環境別プレフィックス等に将来対応するため値で保持）。
type Tables struct {
	Players            string
	Sessions           string
	MatchHistory       string
	MatchmakingTickets string
}

// DefaultTables は LocalStack / dev のデフォルトテーブル名を返す。
func DefaultTables() Tables {
	return Tables{
		Players:            "Players",
		Sessions:           "Sessions",
		MatchHistory:       "MatchHistory",
		MatchmakingTickets: "MatchmakingTickets",
	}
}

// Client は DynamoDB クライアントとテーブル名のセット。
type Client struct {
	ddb    *dynamodb.Client
	tables Tables
}

// NewClient は環境変数から設定を読み、DynamoDB クライアントを構築する。
//
//	AWS_REGION         … リージョン（既定 ap-northeast-1）
//	DYNAMODB_ENDPOINT  … 設定時は LocalStack 等のエンドポイントへ向ける。空なら本物の AWS
func NewClient(ctx context.Context) (*Client, error) {
	region := getenv("AWS_REGION", "ap-northeast-1")
	endpoint := os.Getenv("DYNAMODB_ENDPOINT")

	loadOpts := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}
	if endpoint != "" {
		// LocalStack はダミー認証情報で良い。
		loadOpts = append(loadOpts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("test", "test", ""),
		))
	}

	cfg, err := config.LoadDefaultConfig(ctx, loadOpts...)
	if err != nil {
		return nil, err
	}

	otelaws.AppendMiddlewares(&cfg.APIOptions)

	ddb := dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})

	return &Client{ddb: ddb, tables: DefaultTables()}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// itoa64 は DynamoDB の Number 属性値（文字列表現）へ int64 を変換する。
func itoa64(n int64) string {
	return strconv.FormatInt(n, 10)
}

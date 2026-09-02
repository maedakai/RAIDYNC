// match_status Lambda: GET /match/status/{ticketId}
// ハンドラ本体は internal/handlers にあり、本ファイルは lambda.Start への配線のみを担う。
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
	"github.com/raidync/server/internal/matchmaking"
)

func main() {
	// 構造化ログ(JSON)を既定にする。CloudWatch Logs Insights がフィールド単位で
	// 検索・集計でき、メトリクスフィルタも仕掛けやすくなる。ログ設定のようなグローバル
	// 状態は import した側から見えない副作用になるため、ライブラリではなく main で持つ。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	ctx := context.Background()
	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	matcher := matchmaking.NewMatcher(client, selectSessionProvider(ctx))
	h := handlers.NewMatchStatus(matcher)
	lambda.Start(h.Handle)
}

// selectSessionProvider は GAMELIFT_FLEET_ID が設定されていれば実 GameLift を使い、
// 無ければ GameLiftStub（ローカル/未結線）を返す。GAMELIFT_LOCATION は Anywhere の
// カスタムロケーション（managed EC2 では空）。
func selectSessionProvider(ctx context.Context) matchmaking.SessionProvider {
	fleetID := os.Getenv("GAMELIFT_FLEET_ID")
	if fleetID == "" {
		log.Printf("GAMELIFT_FLEET_ID 未設定: GameLiftStub を使用（ローカル/未結線）")
		return matchmaking.NewGameLiftStub()
	}
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}
	location := os.Getenv("GAMELIFT_LOCATION")
	log.Printf("GameLiftProvider を使用 (fleet=%s location=%q)", fleetID, location)
	return matchmaking.NewGameLiftProviderFromConfig(cfg, fleetID, location)
}

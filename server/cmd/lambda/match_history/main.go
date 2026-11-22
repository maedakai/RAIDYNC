// match_history Lambda: GET /player/{playerId}/history
// ハンドラ本体は internal/handlers にあり、本ファイルは telemetry.StartLambda（OTel 計測付きの lambda.Start）への配線のみを担う。
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
	"github.com/raidync/server/internal/telemetry"
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
	h := handlers.NewMatchHistory(client)
	telemetry.StartLambda("match_history", h.Handle)
}

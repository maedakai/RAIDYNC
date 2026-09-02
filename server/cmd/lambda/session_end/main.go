// session_end Lambda: POST /session/{sessionId}/end
// ハンドラ本体は internal/handlers にあり、本ファイルは lambda.Start への配線のみを担う。
//
// 認証注記: プレイヤーではなくゲームサーバ（GameLift プロセス）が呼ぶ server-to-server
// 経路のため、API Gateway 側でも authorizer を通さない。
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
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
	h := handlers.NewSessionEnd(client)
	lambda.Start(h.Handle)
}

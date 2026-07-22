// session_end Lambda: POST /session/{sessionId}/end
// ハンドラ本体は internal/handlers にあり、本ファイルは lambda.Start への配線のみを担う。
//
// 認証注記: プレイヤーではなくゲームサーバ（GameLift プロセス）が呼ぶ server-to-server
// 経路のため、API Gateway 側でも authorizer を通さない。
package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
)

func main() {
	ctx := context.Background()
	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	h := handlers.NewSessionEnd(client)
	lambda.Start(h.Handle)
}

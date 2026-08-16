// match_request Lambda: POST /match/request
// ハンドラ本体は internal/handlers にあり、本ファイルは lambda.Start への配線のみを担う。
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
	h := handlers.NewMatchRequest(client)
	lambda.Start(h.Handle)
}

// match_status Lambda: GET /match/status/{ticketId}
// ハンドラ本体は internal/handlers にあり、本ファイルは lambda.Start への配線のみを担う。
package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/handlers"
	"github.com/raidync/server/internal/matchmaking"
)

func main() {
	ctx := context.Background()
	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	matcher := matchmaking.NewMatcher(client, matchmaking.NewGameLiftStub())
	h := handlers.NewMatchStatus(matcher)
	lambda.Start(h.Handle)
}

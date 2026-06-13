// match_history Lambda: GET /player/{playerId}/history
// プレイヤーのマッチ履歴を新しい順に返す。
package main

import (
	"context"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

const historyLimit = 20

type handler struct {
	db *db.Client
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	playerID := req.PathParameters["playerId"]
	if playerID == "" {
		return apigw.Error(400, "playerId is required"), nil
	}

	entries, err := h.db.QueryByPlayer(ctx, playerID, historyLimit)
	if err != nil {
		return apigw.Error(500, "history lookup failed"), nil
	}
	return apigw.JSON(200, protocol.MatchHistoryOutput{
		PlayerID: playerID,
		Entries:  entries,
	}), nil
}

func main() {
	ctx := context.Background()
	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	h := &handler{db: client}
	lambda.Start(h.handle)
}

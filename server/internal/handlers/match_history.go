// match_history: GET /player/{playerId}/history
// プレイヤーのマッチ履歴を新しい順に返す。
package handlers

import (
	"context"

	"github.com/aws/aws-lambda-go/events"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

const historyLimit = 20

// MatchHistory は GET /player/{playerId}/history を処理する。
type MatchHistory struct {
	db *db.Client
}

// NewMatchHistory は MatchHistory を生成する。
func NewMatchHistory(client *db.Client) *MatchHistory {
	return &MatchHistory{db: client}
}

// Handle はプレイヤーのマッチ履歴を新しい順に返す。
func (h *MatchHistory) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	playerID := req.PathParameters["playerId"]
	if playerID == "" {
		return apigw.Error(ctx, 400, "playerId is required", nil), nil
	}

	entries, err := h.db.QueryByPlayer(ctx, playerID, historyLimit)
	if err != nil {
		return apigw.Error(ctx, 500, "history lookup failed", err), nil
	}
	return apigw.JSON(ctx, 200, protocol.MatchHistoryOutput{
		PlayerID: playerID,
		Entries:  entries,
	}), nil
}

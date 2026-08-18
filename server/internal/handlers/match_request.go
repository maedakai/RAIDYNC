// match_request: POST /match/request
// チケットを WAITING で発行する。同 player に WAITING 既存があれば冪等にそれを返す。
package handlers

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/google/uuid"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

// MatchRequest は POST /match/request を処理する。
type MatchRequest struct {
	db *db.Client
}

// NewMatchRequest は MatchRequest を生成する。
func NewMatchRequest(client *db.Client) *MatchRequest {
	return &MatchRequest{db: client}
}

// Handle は API Gateway プロキシ統合の signature でマッチリクエストを処理する。
func (h *MatchRequest) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	playerID, ok := apigw.PlayerID(req)
	if !ok {
		return apigw.Error(401, "missing player identity"), nil
	}

	var in protocol.MatchRequestInput
	if err := json.Unmarshal([]byte(req.Body), &in); err != nil {
		return apigw.Error(400, "invalid request body"), nil
	}
	if in.QuestID == "" {
		return apigw.Error(400, "questId is required"), nil
	}

	// 以降の match/status・match/cancel を送るべきリージョン固定 URL。
	// チケットが region-local なため、クライアントはこれに従って接続先を固定する。
	endpoint := apigw.RegionEndpoint(req)

	// 1 プレイヤー 1 チケット: 既存 WAITING があれば再利用（冪等）。
	// 注: この検索は自リージョンのテーブルのみが対象。別リージョンで発行済みの
	// チケットは見えない（region-local の帰結。docs/qa-log.md §13 の不都合②）。
	if existing, err := h.db.FindWaitingByPlayer(ctx, playerID); err != nil {
		return apigw.Error(500, "ticket lookup failed"), nil
	} else if existing != nil {
		return apigw.JSON(200, matchRequestOutput(existing, endpoint)), nil
	}

	now := time.Now()
	nowMs := now.UnixMilli()
	ticket := protocol.Ticket{
		TicketID:    "tkt-" + uuid.NewString(),
		PlayerID:    playerID,
		QuestID:     in.QuestID,
		PartyID:     in.PartyID,
		Status:      protocol.StatusWaiting,
		Version:     0,
		Region:      region(),
		SoloAllowed: in.SoloAllowed,
		LatencyHint: in.LatencyHint,
		CreatedAt:   nowMs,
		ExpiresAt:   nowMs + protocol.MaxWaitSeconds*1000,
		TTL:         now.Unix() + protocol.TicketTTLSeconds,
	}
	if err := h.db.PutTicket(ctx, ticket); err != nil {
		return apigw.Error(500, "failed to create ticket"), nil
	}
	return apigw.JSON(200, matchRequestOutput(&ticket, endpoint)), nil
}

// matchRequestOutput はチケットを応答形に変換する。endpoint は以降のリクエスト先を
// リージョンに固定するためのベース URL（導出できなければ空で、その場合クライアントは
// 従来どおり自分の知っている URL を使い続ける）。
func matchRequestOutput(t *protocol.Ticket, endpoint string) protocol.MatchRequestOutput {
	return protocol.MatchRequestOutput{
		TicketID:  t.TicketID,
		Status:    t.Status,
		CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt,
		Endpoint:  endpoint,
		Region:    t.Region,
	}
}

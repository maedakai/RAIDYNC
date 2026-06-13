// match_request Lambda: POST /match/request
// チケットを WAITING で発行する。同 player に WAITING 既存があれば冪等にそれを返す。
package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/google/uuid"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

type handler struct {
	db *db.Client
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
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

	// 1 プレイヤー 1 チケット: 既存 WAITING があれば再利用（冪等）。
	if existing, err := h.db.FindWaitingByPlayer(ctx, playerID); err != nil {
		return apigw.Error(500, "ticket lookup failed"), nil
	} else if existing != nil {
		return apigw.JSON(200, toOutput(existing)), nil
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
	return apigw.JSON(200, toOutput(&ticket)), nil
}

func toOutput(t *protocol.Ticket) protocol.MatchRequestOutput {
	return protocol.MatchRequestOutput{
		TicketID:  t.TicketID,
		Status:    t.Status,
		CreatedAt: t.CreatedAt,
		ExpiresAt: t.ExpiresAt,
	}
}

func region() string {
	if v := os.Getenv("AWS_REGION"); v != "" {
		return v
	}
	return "ap-northeast-1"
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

// match_status Lambda: GET /match/status/{ticketId}
// チケット状態を返すと同時に、WAITING ならマッチ成立を試みる（マッチャー起動を兼ねる）。
package main

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/matchmaking"
	"github.com/raidync/server/pkg/protocol"
)

type handler struct {
	matcher *matchmaking.Matcher
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ticketID := req.PathParameters["ticketId"]
	if ticketID == "" {
		return apigw.Error(400, "ticketId is required"), nil
	}

	t, err := h.matcher.TryMatch(ctx, ticketID)
	if err != nil {
		if errors.Is(err, matchmaking.ErrTicketNotFound) {
			return apigw.Error(404, "ticket not found"), nil
		}
		return apigw.Error(500, "match processing failed"), nil
	}
	return apigw.JSON(200, statusOutput(t, time.Now().UnixMilli())), nil
}

// statusOutput は状態に応じて有効なフィールドだけを埋めた応答を作る。
func statusOutput(t *protocol.Ticket, nowMs int64) protocol.MatchStatusOutput {
	out := protocol.MatchStatusOutput{TicketID: t.TicketID, Status: t.Status}
	switch t.Status {
	case protocol.StatusWaiting:
		if waited := (nowMs - t.CreatedAt) / 1000; waited > 0 {
			out.WaitedSec = int(waited)
		}
	case protocol.StatusMatched:
		out.SessionID = t.SessionID
		out.ConnectionInfo = t.ConnectionInfo
	}
	return out
}

func main() {
	ctx := context.Background()
	client, err := db.NewClient(ctx)
	if err != nil {
		log.Fatalf("init db client: %v", err)
	}
	matcher := matchmaking.NewMatcher(client, matchmaking.NewGameLiftStub())
	h := &handler{matcher: matcher}
	lambda.Start(h.handle)
}

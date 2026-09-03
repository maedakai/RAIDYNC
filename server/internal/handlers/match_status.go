// match_status: GET /match/status/{ticketId}
// チケット状態を返すと同時に、WAITING ならマッチ成立を試みる（マッチャー起動を兼ねる）。
package handlers

import (
	"context"
	"errors"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/matchmaking"
	"github.com/raidync/server/pkg/protocol"
)

// MatchStatus は GET /match/status/{ticketId} を処理する。
type MatchStatus struct {
	matcher *matchmaking.Matcher
}

// NewMatchStatus は MatchStatus を生成する。
func NewMatchStatus(matcher *matchmaking.Matcher) *MatchStatus {
	return &MatchStatus{matcher: matcher}
}

// Handle はチケット状態を返しつつ、WAITING ならマッチ成立を試みる。
func (h *MatchStatus) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ticketID := req.PathParameters["ticketId"]
	if ticketID == "" {
		return apigw.Error(ctx, 400, "ticketId is required", nil), nil
	}

	t, err := h.matcher.TryMatch(ctx, ticketID)
	if err != nil {
		if errors.Is(err, matchmaking.ErrTicketNotFound) {
			return apigw.Error(ctx, 404, "ticket not found", nil), nil
		}
		return apigw.Error(ctx, 500, "match processing failed", err), nil
	}
	return apigw.JSON(ctx, 200, statusOutput(t, time.Now().UnixMilli())), nil
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

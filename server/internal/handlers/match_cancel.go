// match_cancel: DELETE /match/cancel/{ticketId}
// WAITING のチケットのみ CANCELLED にする。MATCHING/MATCHED は 409、不在は 404。
package handlers

import (
	"context"
	"errors"

	"github.com/aws/aws-lambda-go/events"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

// MatchCancel は DELETE /match/cancel/{ticketId} を処理する。
type MatchCancel struct {
	db *db.Client
}

// NewMatchCancel は MatchCancel を生成する。
func NewMatchCancel(client *db.Client) *MatchCancel {
	return &MatchCancel{db: client}
}

// Handle は WAITING チケットをキャンセルする。
func (h *MatchCancel) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ticketID := req.PathParameters["ticketId"]
	if ticketID == "" {
		return apigw.Error(ctx, 400, "ticketId is required", nil), nil
	}

	t, err := h.db.GetTicket(ctx, ticketID)
	if err != nil {
		return apigw.Error(ctx, 500, "ticket lookup failed", err), nil
	}
	if t == nil {
		return apigw.Error(ctx, 404, "ticket not found", nil), nil
	}
	if t.Status != protocol.StatusWaiting {
		return apigw.Error(ctx, 409, "ticket is not cancellable", nil), nil
	}

	// 条件付き更新で WAITING 以外への遷移と競合した場合も 409 に倒す。
	if err := h.db.CancelTicket(ctx, ticketID); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return apigw.Error(ctx, 409, "ticket is not cancellable", nil), nil
		}
		return apigw.Error(ctx, 500, "cancel failed", err), nil
	}
	return apigw.JSON(ctx, 200, map[string]string{
		"ticketId": ticketID,
		"status":   string(protocol.StatusCancelled),
	}), nil
}

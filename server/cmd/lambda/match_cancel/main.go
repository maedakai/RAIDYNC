// match_cancel Lambda: DELETE /match/cancel/{ticketId}
// WAITING のチケットのみ CANCELLED にする。MATCHING/MATCHED は 409、不在は 404。
package main

import (
	"context"
	"errors"
	"log"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

type handler struct {
	db *db.Client
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	ticketID := req.PathParameters["ticketId"]
	if ticketID == "" {
		return apigw.Error(400, "ticketId is required"), nil
	}

	t, err := h.db.GetTicket(ctx, ticketID)
	if err != nil {
		return apigw.Error(500, "ticket lookup failed"), nil
	}
	if t == nil {
		return apigw.Error(404, "ticket not found"), nil
	}
	if t.Status != protocol.StatusWaiting {
		return apigw.Error(409, "ticket is not cancellable"), nil
	}

	// 条件付き更新で WAITING 以外への遷移と競合した場合も 409 に倒す。
	if err := h.db.CancelTicket(ctx, ticketID); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return apigw.Error(409, "ticket is not cancellable"), nil
		}
		return apigw.Error(500, "cancel failed"), nil
	}
	return apigw.JSON(200, map[string]string{
		"ticketId": ticketID,
		"status":   string(protocol.StatusCancelled),
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

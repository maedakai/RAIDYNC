// session_end: POST /session/{sessionId}/end
// ゲームサーバがセッション終了時に結果を報告する。セッションの全プレイヤー分の
// マッチ履歴の追記と ENDED への遷移を、1 つのトランザクションで行う。ACTIVE のセッションだけを受け付ける。
//
// 認証注記: 本エンドポイントはプレイヤーではなくゲームサーバ（本番では GameLift
// プロセス）が呼ぶ server-to-server 経路。本番ではフリートの IAM ロール／内部
// ネットワークで保護する前提で、プレイヤー JWT は要求しない。
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/raidync/server/internal/apigw"
	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

// SessionEnd は POST /session/{sessionId}/end を処理する。
type SessionEnd struct {
	db *db.Client
}

// NewSessionEnd は SessionEnd を生成する。
func NewSessionEnd(client *db.Client) *SessionEnd {
	return &SessionEnd{db: client}
}

// Handle はセッション結果を全プレイヤーの履歴に保存し、セッションを終了する。
func (h *SessionEnd) Handle(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	sessionID := req.PathParameters["sessionId"]
	if sessionID == "" {
		return apigw.Error(ctx, 400, "sessionId is required", nil), nil
	}

	var in protocol.SessionEndInput
	if err := json.Unmarshal([]byte(req.Body), &in); err != nil {
		return apigw.Error(ctx, 400, "invalid request body", nil), nil
	}
	if !validResult(in.Result) {
		return apigw.Error(ctx, 400, "result must be one of CLEAR/FAILED/ABANDONED", nil), nil
	}

	sess, err := h.db.GetSession(ctx, sessionID)
	if err != nil {
		return apigw.Error(ctx, 500, "session lookup failed", err), nil
	}
	if sess == nil {
		return apigw.Error(ctx, 404, "session not found", nil), nil
	}
	// ACTIVE 以外（ENDED＝二重の報告、FAILED＝マッチの確定に失敗して片付け済み）は受け付けない。
	// ここは無駄なトランザクションを省くための早めの確認で、本当の守りは EndSession の条件。
	if sess.Status != protocol.SessionActive {
		return apigw.Error(ctx, 409, "session is not active", nil), nil
	}

	// 全員分の履歴と ENDED への遷移を、1 つのトランザクションで書く。
	// 読んだ後に別の経路で ENDED / FAILED になっていれば、条件に合わず何も書かれない（409）。
	// 途中で落ちても何も残らないため、報告し直せばやり直せる。
	now := time.Now().UnixMilli()
	entries := make([]protocol.MatchHistoryEntry, 0, len(sess.PlayerIDs))
	for _, playerID := range sess.PlayerIDs {
		entries = append(entries, protocol.MatchHistoryEntry{
			PlayerID:    playerID,
			Timestamp:   now,
			SessionID:   sessionID,
			QuestID:     sess.QuestID,
			Result:      in.Result,
			DurationSec: in.DurationSec,
		})
	}

	if err := h.db.EndSession(ctx, sessionID, entries, now); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return apigw.Error(ctx, 409, "session is not active", nil), nil
		}
		return apigw.Error(ctx, 500, "failed to end session", err), nil
	}

	return apigw.JSON(ctx, 200, protocol.SessionEndOutput{
		SessionID:    sessionID,
		Status:       protocol.SessionEnded,
		PlayersSaved: len(sess.PlayerIDs),
	}), nil
}

func validResult(r protocol.MatchResult) bool {
	switch r {
	case protocol.ResultClear, protocol.ResultFailed, protocol.ResultAbandoned:
		return true
	default:
		return false
	}
}

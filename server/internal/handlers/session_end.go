// session_end: POST /session/{sessionId}/end
// ゲームサーバがセッション終了時に結果を報告する。セッションの全プレイヤー分の
// マッチ履歴を 1 件ずつ追記し、最後にセッションを ENDED へ遷移する。
//
// 認証注記: 本エンドポイントはプレイヤーではなくゲームサーバ（本番では GameLift
// プロセス）が呼ぶ server-to-server 経路。本番ではフリートの IAM ロール／内部
// ネットワークで保護する前提で、プレイヤー JWT は要求しない。
package handlers

import (
	"context"
	"encoding/json"
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
		return apigw.Error(400, "sessionId is required"), nil
	}

	var in protocol.SessionEndInput
	if err := json.Unmarshal([]byte(req.Body), &in); err != nil {
		return apigw.Error(400, "invalid request body"), nil
	}
	if !validResult(in.Result) {
		return apigw.Error(400, "result must be one of CLEAR/FAILED/ABANDONED"), nil
	}

	sess, err := h.db.GetSession(ctx, sessionID)
	if err != nil {
		return apigw.Error(500, "session lookup failed"), nil
	}
	if sess == nil {
		return apigw.Error(404, "session not found"), nil
	}
	// 既に終了済みなら二重報告として弾く（冪等性ガード）。
	if sess.Status == protocol.SessionEnded {
		return apigw.Error(409, "session already ended"), nil
	}

	// 履歴を先に全員分書き、最後にセッションを終了する。
	// （途中失敗時はセッションが ACTIVE のまま残り、再報告で復旧できる側に倒す。）
	now := time.Now().UnixMilli()
	for _, playerID := range sess.PlayerIDs {
		entry := protocol.MatchHistoryEntry{
			PlayerID:    playerID,
			Timestamp:   now,
			SessionID:   sessionID,
			QuestID:     sess.QuestID,
			Result:      in.Result,
			DurationSec: in.DurationSec,
		}
		if err := h.db.PutMatchResult(ctx, entry); err != nil {
			return apigw.Error(500, "failed to save match result"), nil
		}
	}

	if err := h.db.EndSession(ctx, sessionID, now); err != nil {
		return apigw.Error(500, "failed to end session"), nil
	}

	return apigw.JSON(200, protocol.SessionEndOutput{
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

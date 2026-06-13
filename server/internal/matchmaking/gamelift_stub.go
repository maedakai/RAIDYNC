package matchmaking

import (
	"context"

	"github.com/google/uuid"

	"github.com/raidync/server/pkg/protocol"
)

// SessionProvider はゲームセッション生成を抽象化する。
// Phase 2 は GameLiftStub が固定接続先を返し、Phase 3 で本物の GameLift クライアントに差し替える。
type SessionProvider interface {
	CreateGameSession(ctx context.Context, questID string, playerIDs []string) (sessionID string, conn protocol.ConnectionInfo, err error)
}

// GameLiftStub は Phase 2 用のスタブ。GameLift Local / ダミー接続先を返す。
type GameLiftStub struct {
	Host string
	Port int
}

// NewGameLiftStub は既定で 127.0.0.1:7777 を返すスタブを生成する。
func NewGameLiftStub() *GameLiftStub {
	return &GameLiftStub{Host: "127.0.0.1", Port: 7777}
}

// CreateGameSession は固定ホストと採番した ID で接続情報を返す。
// 本物の GameLift は playerSessionId をプレイヤー毎に発行するが、スタブでは 1 つで代用する。
func (s *GameLiftStub) CreateGameSession(_ context.Context, _ string, _ []string) (string, protocol.ConnectionInfo, error) {
	sessionID := "sess-" + uuid.NewString()
	conn := protocol.ConnectionInfo{
		IPAddress:       s.Host,
		Port:            s.Port,
		PlayerSessionID: "psess-" + uuid.NewString(),
	}
	return sessionID, conn, nil
}

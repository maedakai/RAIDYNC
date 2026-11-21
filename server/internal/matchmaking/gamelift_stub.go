package matchmaking

import (
	"context"

	"github.com/google/uuid"

	"github.com/raidync/server/pkg/protocol"
)

// SessionProvider はゲームセッションの生成と終了を抽象化する。
// sessionID は呼び出し側が決める（マッチ確保時に書いた IdempotencyToken）。どの実装もこの値を
// そのままセッションの ID にし、返り値の id も同じ値になる。救済する側がチケットのトークンだけで
// セッションを特定できるようにするため。
// 返り値の conn は全プレイヤー共通の接続先（IP/Port）で PlayerSessionID は空。
// playerSessions は playerId→playerSessionId の対応表で、GameLift の 1 プレイヤー 1 枠
// （CreatePlayerSession でプレイヤー毎に発行される playerSessionId）を表す。呼び出し側は
// 各プレイヤーへ「共通 conn ＋ 自分の psid」を配る。
// EndGameSession は、セッションが無い・すでに終わっている場合も成功（nil）を返す。
// 同じグループの複数のポーリングから同時に呼ばれてもよい。
//
// 実装: ローカルは GameLiftStub / hubSessionProvider が、本番は実 GameLift クライアント（G2 後半）。
type SessionProvider interface {
	CreateGameSession(ctx context.Context, sessionID, questID string, playerIDs []string) (id string, conn protocol.ConnectionInfo, playerSessions map[string]string, err error)
	EndGameSession(ctx context.Context, sessionID string) error
}

// GameLiftStub は固定接続先を返すスタブ（実 GameLift クライアント未結線時のローカル用）。
type GameLiftStub struct {
	Host string
	Port int
}

// NewGameLiftStub は既定で 127.0.0.1:7777 を返すスタブを生成する。
func NewGameLiftStub() *GameLiftStub {
	return &GameLiftStub{Host: "127.0.0.1", Port: 7777}
}

// CreateGameSession は固定ホストと、受け取った sessionID で接続情報を返す。
// 本物の GameLift 同様、プレイヤー毎に別々の playerSessionId を採番する。
func (s *GameLiftStub) CreateGameSession(_ context.Context, sessionID, _ string, playerIDs []string) (string, protocol.ConnectionInfo, map[string]string, error) {
	conn := protocol.ConnectionInfo{IPAddress: s.Host, Port: s.Port}
	playerSessions := make(map[string]string, len(playerIDs))
	for _, pid := range playerIDs {
		playerSessions[pid] = "psess-" + uuid.NewString()
	}
	return sessionID, conn, playerSessions, nil
}

// EndGameSession は終わらせるセッションを持たないので、何もせず成功を返す。
func (s *GameLiftStub) EndGameSession(_ context.Context, _ string) error {
	return nil
}

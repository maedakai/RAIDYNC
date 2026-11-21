// gameserver.go は localapi に UDP ゲームサーバ(Hub)を同居させ、マッチ成立で確定した
// roster をその Hub に動的登録するための配線を持つ。これは本番 GameLift の
// OnStartGameSession コールバック（マッチ結果の roster をサーバプロセスへ注入）の
// ローカル代役にあたる。差し替え点は matchmaking.SessionProvider インターフェース。
package main

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/raidync/server/internal/game"
	"github.com/raidync/server/internal/gameserver"
	"github.com/raidync/server/pkg/protocol"
)

// loggingObserver は game.PlayerObserver のローカル実装。実 AWS の GameLiftBridge
// （OnPlayerJoin→AcceptPlayerSession / OnPlayerLeave→RemovePlayerSession を実 GameLift へ
// 送る）の代役として、参加/離脱と提示された psid をログに出すだけの No-op。これにより
// 「クライアント提示 psid → サーバ採用 → observer 到達」の一気通貫を実 AWS 無しで確認できる。
type loggingObserver struct{}

func (loggingObserver) OnPlayerJoin(sessionID, playerID, playerSessionID string) {
	log.Printf("gameserver: player join session=%s player=%s psid=%s (local: AcceptPlayerSession 相当)",
		sessionID, playerID, orNone(playerSessionID))
}

func (loggingObserver) OnPlayerLeave(sessionID, playerID, playerSessionID string) {
	log.Printf("gameserver: player leave session=%s player=%s psid=%s (local: RemovePlayerSession 相当)",
		sessionID, playerID, orNone(playerSessionID))
}

// orNone は空 psid を可読な "(none)" に置き換える（提示漏れをログで見つけやすくする）。
func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// hubSessionProvider は matchmaking.SessionProvider を満たす。マッチ成立時に確定した
// playerIDs を roster とする game.Session を生成し、同一プロセスで動く Hub に登録する。
// 返す ConnectionInfo はその Hub が listen している UDP の接続先。
type hubSessionProvider struct {
	hub  *game.Hub
	host string // クライアントへ知らせる接続先 IP（ローカルは 127.0.0.1）
	port int    // UDP ポート
}

// CreateGameSession は本番 GameLift CreateGameSession + CreatePlayerSessions の代役。
// 受け取った sessionID で roster 付きセッションを Hub に登録して接続先を返す。プレイヤー毎に
// psid を採番して返す（本番の CreatePlayerSessions 相当）。各クライアントはこの psid を
// match/status で受け取り、UDP 接続時に提示する。セッションには loggingObserver を注入し、
// 提示 psid が参加/離脱で observer まで到達する様子をローカルで観測できるようにする。
// 対応表(WithPlayerSessions)は事前投入しない＝実フロー同様、psid はクライアント提示で埋まる。
func (p *hubSessionProvider) CreateGameSession(_ context.Context, sessionID, questID string, playerIDs []string) (string, protocol.ConnectionInfo, map[string]string, error) {
	p.hub.Register(game.NewSession(sessionID, questID, playerIDs, time.Now().UnixMilli(),
		game.WithPlayerObserver(loggingObserver{})))
	conn := protocol.ConnectionInfo{IPAddress: p.host, Port: p.port}
	playerSessions := make(map[string]string, len(playerIDs))
	for _, pid := range playerIDs {
		playerSessions[pid] = "psess-" + uuid.NewString()
	}
	return sessionID, conn, playerSessions, nil
}

// EndGameSession は本番 GameLift TerminateGameSession の代役。Hub からセッションを外し、
// 接続中のプレイヤーに SESSION_END を送る。セッションが無ければ何もせず成功を返す。
func (p *hubSessionProvider) EndGameSession(_ context.Context, sessionID string) error {
	p.hub.End(sessionID, time.Now().UnixMilli())
	return nil
}

// startGameServer は GameLift モード（固定セッションを作らない）で UDP ゲームサーバを
// 起動し、Serve を ctx 連動でバックグラウンド駆動する。返す provider をマッチャーに渡すと、
// マッチ成立でこの Hub にセッションが登録され、クライアントは advertised host:port へ
// UDP 接続できる。ctx キャンセルで Serve は停止する。
func startGameServer(ctx context.Context, bindAddr, advertiseHost string) (*hubSessionProvider, *gameserver.Server, error) {
	srv, err := gameserver.New(gameserver.Config{
		BindAddr:        bindAddr,
		GameLiftEnabled: true, // 固定セッションを作らず、provider が動的に登録する
	})
	if err != nil {
		return nil, nil, err
	}
	go srv.Serve(ctx)

	addr := srv.LocalAddr()
	provider := &hubSessionProvider{
		hub:  srv.Hub(),
		host: advertiseHost,
		port: addr.Port, // BindAddr がポート 0 でも実ポートを広告できる
	}
	return provider, srv, nil
}

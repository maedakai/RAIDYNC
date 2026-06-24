// gameserver.go は localapi に UDP ゲームサーバ(Hub)を同居させ、マッチ成立で確定した
// roster をその Hub に動的登録するための配線を持つ。これは本番 GameLift の
// OnStartGameSession コールバック（マッチ結果の roster をサーバプロセスへ注入）の
// ローカル代役にあたる。差し替え点は matchmaking.SessionProvider インターフェース。
package main

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/raidync/server/internal/game"
	"github.com/raidync/server/internal/gameserver"
	"github.com/raidync/server/pkg/protocol"
)

// hubSessionProvider は matchmaking.SessionProvider を満たす。マッチ成立時に確定した
// playerIDs を roster とする game.Session を生成し、同一プロセスで動く Hub に登録する。
// 返す ConnectionInfo はその Hub が listen している UDP の接続先。
type hubSessionProvider struct {
	hub  *game.Hub
	host string // クライアントへ知らせる接続先 IP（ローカルは 127.0.0.1）
	port int    // UDP ポート
}

// CreateGameSession は本番 GameLift CreateGameSession の代役。
// sessionID を採番し、roster 付きセッションを Hub に登録して接続先を返す。
func (p *hubSessionProvider) CreateGameSession(_ context.Context, questID string, playerIDs []string) (string, protocol.ConnectionInfo, error) {
	sessionID := "sess-" + uuid.NewString()
	p.hub.Register(game.NewSession(sessionID, questID, playerIDs, time.Now().UnixMilli()))
	conn := protocol.ConnectionInfo{
		IPAddress:       p.host,
		Port:            p.port,
		PlayerSessionID: "psess-" + uuid.NewString(),
	}
	return sessionID, conn, nil
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

// Command gameserver は RAIDYNC のリアルタイムゲームサーバー（UDP）。
//
// Unity クライアントからの PLAYER_MOVE / PLAYER_ACTION を UDP で受信し、
// 50ms 周期で全プレイヤーへ SESSION_STATE をブロードキャストする（spec-game.md §3）。
// 信頼性レイヤを持たない UDP を選択しているため、順序制御は各メッセージの seq で行う
// （internal/game の Session が古い/重複パケットを破棄する）。
//
// 配線とループ駆動は internal/gameserver に置き、ここは設定読み込みと起動のみ。
// GameLift Server SDK 連携は Phase 3 後半（ステップ5）で追加する。
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/raidync/server/internal/gameserver"
)

func main() {
	cfg := loadConfig()

	srv, err := gameserver.New(cfg)
	if err != nil {
		log.Fatalf("start gameserver: %v", err)
	}
	defer srv.Close()
	log.Printf("gameserver listening on udp %s (session=%q quest=%s roster=%v)",
		srv.LocalAddr(), cfg.SessionID, cfg.QuestID, cfg.Roster)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// GameLift モードでは SDK を起動し、セッションはコールバックで動的に作られる。
	// ローカルモード（既定）では起動せず、固定セッションで動く。
	var bridge *gameserver.GameLiftBridge
	if cfg.GameLiftEnabled {
		bridge = gameserver.NewGameLiftBridge(srv.Hub(), srv.LocalAddr().Port, stop)
		if err := bridge.Activate(); err != nil {
			log.Fatalf("gamelift activate: %v", err)
		}
		log.Printf("GameLift mode: ready, waiting for game sessions")
	}

	srv.Serve(ctx)

	if bridge != nil {
		if err := bridge.End(); err != nil {
			log.Printf("ProcessEnding: %v", err)
		}
	}
	log.Printf("gameserver stopped")
}

func loadConfig() gameserver.Config {
	return gameserver.Config{
		BindAddr:        env("GAMESERVER_BIND", ":7777"),
		SessionID:       env("GAMESERVER_SESSION_ID", "local-session-1"),
		QuestID:         env("GAMESERVER_QUEST_ID", "quest-001"),
		Roster:          splitRoster(env("GAMESERVER_ROSTER", "p1,p2,p3,p4")),
		GameLiftEnabled: env("GAMELIFT_ENABLED", "false") == "true",
	}
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func splitRoster(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

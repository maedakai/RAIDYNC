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

	"github.com/raidync/server/internal/game"
	"github.com/raidync/server/internal/gameserver"
)

func main() {
	cfg := loadConfig()

	// SESSION_API_URL があれば、セッションが終わったときに /session/end で結果を報告する。
	// 空なら報告しない（ローカルでゲームサーバーだけを動かす場合）。
	var reporter *gameserver.SessionReporter
	var hubOpts []game.HubOption
	if cfg.SessionAPIURL != "" {
		reporter = gameserver.NewSessionReporter(cfg.SessionAPIURL)
		hubOpts = append(hubOpts, game.WithSessionEndObserver(reporter))
	}

	srv, err := gameserver.New(cfg, hubOpts...)
	if err != nil {
		log.Fatalf("start gameserver: %v", err)
	}
	defer srv.Close()
	log.Printf("gameserver listening on udp %s (session=%q quest=%s roster=%v report=%t)",
		srv.LocalAddr(), cfg.SessionID, cfg.QuestID, cfg.Roster, reporter != nil)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// GameLift モードでは SDK を起動し、セッションはコールバックで動的に作られる。
	// ローカルモード（既定）では起動せず、固定セッションで動く。
	var bridge *gameserver.GameLiftBridge
	if cfg.GameLiftEnabled {
		bridge = gameserver.NewGameLiftBridge(srv.Hub(), srv.LocalAddr().Port, stop)
		// セッションが終わったらプロセスを終える（1 プロセス 1 セッション）。
		// Bridge は Hub を受け取って後から作られるので、通知先は作った後に足す。
		srv.Hub().AddSessionEndObserver(bridge)
		if err := bridge.Activate(); err != nil {
			log.Fatalf("gamelift activate: %v", err)
		}
		log.Printf("GameLift mode: ready, waiting for game sessions")
	}

	srv.Serve(ctx)

	// 終わりの順番: 報告を待つ → ProcessEnding → ソケットを閉じる（defer）。
	// ProcessEnding を先に呼ぶと、GameLift がプロセスを片付け始め、送りかけの報告が消えうる。
	if reporter != nil {
		reporter.Wait()
	}
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
		SessionAPIURL:   env("SESSION_API_URL", ""),
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

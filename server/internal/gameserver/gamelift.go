package gameserver

import (
	"context"
	"log"
	"strings"

	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/model"
	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/server"

	"github.com/raidync/server/internal/game"
)

// GameLiftBridge は GameLift Server SDK 5.x のライフサイクルを Hub に橋渡しする。
// InitSDK → ProcessReady でコールバックを登録し、OnStartGameSession で GameLift から
// 渡る GameSession を game.Session に変換して Hub に登録する。
//
// RAIDYNC は FlexMatch を使わず自前マッチメイキング（Lambda + DynamoDB）なので、
// roster / questId は MatchmakerData ではなく GameProperties（key:value）で受け渡す
// 想定（Lambda 側が CreateGameSession 時に詰める）。
type GameLiftBridge struct {
	hub  *game.Hub
	port int
	stop context.CancelFunc // OnProcessTerminate で受信ループを止める
}

// NewGameLiftBridge は Bridge を生成する。stop は Serve のコンテキストを止める CancelFunc。
func NewGameLiftBridge(hub *game.Hub, port int, stop context.CancelFunc) *GameLiftBridge {
	return &GameLiftBridge{hub: hub, port: port, stop: stop}
}

// Activate は SDK を初期化し、GameLift にプロセス準備完了（ProcessReady）を通知する。
// 認証情報は managed EC2 の環境、または Anywhere 用の環境変数から取得する。
func (b *GameLiftBridge) Activate() error {
	if err := server.InitSDKFromEnvironment(); err != nil {
		return err
	}
	return server.ProcessReady(server.ProcessParameters{
		OnStartGameSession: b.onStartGameSession,
		OnProcessTerminate: b.onProcessTerminate,
		OnHealthCheck:      b.onHealthCheck,
		Port:               b.port,
	})
}

// End は全クリーンアップ後に GameLift へプロセス終了（ProcessEnding）を通知する。
func (b *GameLiftBridge) End() error { return server.ProcessEnding() }

// onStartGameSession は GameLift からの新規セッション要求を Hub に反映し、
// プレイヤー接続受け入れ可能（ActivateGameSession）を通知する。
func (b *GameLiftBridge) onStartGameSession(gs model.GameSession) {
	questID, roster := parseSessionConfig(gs)
	b.hub.Register(game.NewSession(gs.GameSessionID, questID, roster, nowMs()))
	if err := server.ActivateGameSession(); err != nil {
		log.Printf("ActivateGameSession %s: %v", gs.GameSessionID, err)
		return
	}
	log.Printf("game session started: %s (quest=%s roster=%v)", gs.GameSessionID, questID, roster)
}

// onProcessTerminate は GameLift からの停止要求（Spot 中断/スケールダウン）で呼ばれる。
// 受信ループを止め、main 側が End()→ProcessEnding を呼べるようにする。
func (b *GameLiftBridge) onProcessTerminate() {
	log.Printf("OnProcessTerminate received; draining and shutting down")
	b.stop()
}

// onHealthCheck は 60 秒ごとの死活監視。現状は常に健全を返す。
func (b *GameLiftBridge) onHealthCheck() bool { return true }

// parseSessionConfig は GameProperties から questId と roster を取り出す純関数。
// SDK の型に依存するが副作用がないのでユニットテスト可能。
func parseSessionConfig(gs model.GameSession) (questID string, roster []string) {
	questID = gs.GameProperties["questId"]
	if questID == "" {
		questID = "unknown"
	}
	for _, p := range strings.Split(gs.GameProperties["playerIds"], ",") {
		if p = strings.TrimSpace(p); p != "" {
			roster = append(roster, p)
		}
	}
	return questID, roster
}

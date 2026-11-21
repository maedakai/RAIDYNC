package gameserver

import (
	"context"
	"log"
	"strings"

	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/model"
	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/server"

	"github.com/raidync/server/internal/game"
	"github.com/raidync/server/pkg/protocol"
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

	// events は参加/離脱イベントを 1 本のワーカーに直列化して渡す。SDK の
	// AcceptPlayerSession/RemovePlayerSession はネットワーク I/O なので、UDP 受信や
	// Tick のホットパスで直接呼ばず、ここに積んでワーカー側で順序どおり実行する。
	events chan playerEvent
	done   chan struct{}
}

// playerEvent は 1 プレイヤーの参加(join=true)/離脱(join=false)を表す。
type playerEvent struct {
	join            bool
	sessionID       string
	playerID        string
	playerSessionID string
}

// NewGameLiftBridge は Bridge を生成する。stop は Serve のコンテキストを止める CancelFunc。
func NewGameLiftBridge(hub *game.Hub, port int, stop context.CancelFunc) *GameLiftBridge {
	return &GameLiftBridge{
		hub:    hub,
		port:   port,
		stop:   stop,
		events: make(chan playerEvent, 64),
		done:   make(chan struct{}),
	}
}

// Activate は SDK を初期化し、GameLift にプロセス準備完了（ProcessReady）を通知する。
// 認証情報は managed EC2 の環境、または Anywhere 用の環境変数から取得する。
func (b *GameLiftBridge) Activate() error {
	if err := server.InitSDKFromEnvironment(); err != nil {
		return err
	}
	go b.processEvents()
	return server.ProcessReady(server.ProcessParameters{
		OnStartGameSession: b.onStartGameSession,
		OnProcessTerminate: b.onProcessTerminate,
		OnHealthCheck:      b.onHealthCheck,
		Port:               b.port,
	})
}

// End はイベントワーカーを止め、クリーンアップ後に GameLift へプロセス終了（ProcessEnding）を通知する。
func (b *GameLiftBridge) End() error {
	close(b.events)
	<-b.done
	return server.ProcessEnding()
}

// processEvents は参加/離脱イベントを 1 件ずつ SDK 呼び出しに変換する（順序保証）。
// playerSessionId が未確定（ローカル/G2 未結線）の場合はスキップする。
func (b *GameLiftBridge) processEvents() {
	defer close(b.done)
	for e := range b.events {
		if e.playerSessionID == "" {
			log.Printf("skip SDK call: playerSessionId 未確定 (session=%s player=%s join=%v)", e.sessionID, e.playerID, e.join)
			continue
		}
		if e.join {
			if err := server.AcceptPlayerSession(e.playerSessionID); err != nil {
				log.Printf("AcceptPlayerSession %s (player=%s): %v", e.playerSessionID, e.playerID, err)
			}
		} else {
			if err := server.RemovePlayerSession(e.playerSessionID); err != nil {
				log.Printf("RemovePlayerSession %s (player=%s): %v", e.playerSessionID, e.playerID, err)
			}
		}
	}
}

// OnPlayerJoin / OnPlayerLeave は game.PlayerObserver を満たす。Session の Mutex 外で
// 呼ばれる前提。ここではキューに積むだけで即座に返し、ホットパスを止めない。
func (b *GameLiftBridge) OnPlayerJoin(sessionID, playerID, playerSessionID string) {
	b.enqueue(playerEvent{join: true, sessionID: sessionID, playerID: playerID, playerSessionID: playerSessionID})
}

func (b *GameLiftBridge) OnPlayerLeave(sessionID, playerID, playerSessionID string) {
	b.enqueue(playerEvent{join: false, sessionID: sessionID, playerID: playerID, playerSessionID: playerSessionID})
}

// enqueue はイベントを積む。キュー満杯時は落とす（ゲーム進行を止めないことを優先）。
func (b *GameLiftBridge) enqueue(e playerEvent) {
	select {
	case b.events <- e:
	default:
		log.Printf("player event dropped (queue full): session=%s player=%s join=%v", e.sessionID, e.playerID, e.join)
	}
}

// onStartGameSession は GameLift からの新規セッション要求を Hub に反映し、
// プレイヤー接続受け入れ可能（ActivateGameSession）を通知する。
func (b *GameLiftBridge) onStartGameSession(gs model.GameSession) {
	questID, roster, playerSessions := parseSessionConfig(gs)
	// GameLift SDK は GameSessionID を ARN で渡す。Lambda 側（gamelift_provider.go）が
	// 同じ正規化でショート ID（ARN 末尾 = マッチ確保時のトークン）を DynamoDB / クライアントへ配っているため、
	// Hub のルーティングキーもショート ID に揃える（不一致だと ErrSessionNotFound になる）。
	sessionID := protocol.NormalizeGameSessionID(gs.GameSessionID)
	b.hub.Register(game.NewSession(sessionID, questID, roster, nowMs(),
		game.WithPlayerObserver(b),
		game.WithPlayerSessions(playerSessions),
	))
	if err := server.ActivateGameSession(); err != nil {
		log.Printf("ActivateGameSession %s: %v", sessionID, err)
		return
	}
	log.Printf("game session started: %s (quest=%s roster=%v psids=%d)", sessionID, questID, roster, len(playerSessions))
}

// onProcessTerminate は GameLift からの停止要求（Spot 中断/スケールダウン）で呼ばれる。
// 受信ループを止め、main 側が End()→ProcessEnding を呼べるようにする。
func (b *GameLiftBridge) onProcessTerminate() {
	log.Printf("OnProcessTerminate received; draining and shutting down")
	b.stop()
}

// onHealthCheck は 60 秒ごとの死活監視。現状は常に健全を返す。
func (b *GameLiftBridge) onHealthCheck() bool { return true }

// parseSessionConfig は GameProperties から questId / roster / playerId→psid 対応表を
// 取り出す純関数。SDK の型に依存するが副作用がないのでユニットテスト可能。
//
// GameProperties の想定:
//   - questId       : "quest-rathalos"
//   - playerIds     : "p1,p2,p3,p4"（カンマ区切り。roster=参加許可 playerId）
//   - playerSessions : "p1=psess-1,p2=psess-2"（任意。Accept/RemovePlayerSession 用の
//     playerId→playerSessionId。実 PlayerSession 発行(G2)まではローカルで空になり得る）
func parseSessionConfig(gs model.GameSession) (questID string, roster []string, playerSessions map[string]string) {
	questID = gs.GameProperties["questId"]
	if questID == "" {
		questID = "unknown"
	}
	for _, p := range strings.Split(gs.GameProperties["playerIds"], ",") {
		if p = strings.TrimSpace(p); p != "" {
			roster = append(roster, p)
		}
	}
	playerSessions = make(map[string]string)
	for _, pair := range strings.Split(gs.GameProperties["playerSessions"], ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		pid, psid, ok := strings.Cut(pair, "=")
		pid, psid = strings.TrimSpace(pid), strings.TrimSpace(psid)
		if ok && pid != "" && psid != "" {
			playerSessions[pid] = psid
		}
	}
	return questID, roster, playerSessions
}

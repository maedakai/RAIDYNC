package protocol

import "encoding/json"

// ---------------------------------------------------------------------------
// ゲームサーバー定数（spec-game.md §3）
// ---------------------------------------------------------------------------

const (
	StateSyncHz         = 20            // SESSION_STATE のブロードキャスト頻度（回/秒）
	StateSyncIntervalMs = 1000 / StateSyncHz // 50ms
	MaxSessionMinutes   = 20            // 1 セッションの最大時間
	PlayerIdleTimeoutMs = 10_000        // この時間パケットが来なければ離脱とみなす
)

// ---------------------------------------------------------------------------
// メッセージ種別（C# と Go の共通フォーマット, spec-game.md §3.4）
// ---------------------------------------------------------------------------

// GameMessageType はゲーム内 UDP メッセージの種別。
type GameMessageType string

const (
	MsgPlayerMove   GameMessageType = "PLAYER_MOVE"   // C# → Go: 位置・回転
	MsgPlayerAction GameMessageType = "PLAYER_ACTION" // C# → Go: 攻撃・スキル等
	MsgSessionState GameMessageType = "SESSION_STATE" // Go → C#: 全プレイヤー状態（ブロードキャスト）
	MsgSessionEnd   GameMessageType = "SESSION_END"   // Go → C#: セッション終了通知
)

// GameMessage は spec-game.md §3.4 の共通エンベロープ。
// payload は type ごとに中身が変わるため RawMessage で受け、型別に再デコードする。
//
// Seq は spec の素の定義にはないが、UDP は順序保証・重複排除がないため本実装で付与する
// 拡張フィールド。クライアントは 1 始まりの単調増加で採番し、サーバーはプレイヤー単位で
// 受理済み最大値を保持して、古い/重複パケットを破棄する（位置同期の並べ替え対策）。
type GameMessage struct {
	Type      GameMessageType `json:"type"`
	SessionID string          `json:"sessionId"`
	PlayerID  string          `json:"playerId"`
	Timestamp int64           `json:"timestamp"` // UnixTime ms
	Seq       uint32          `json:"seq"`       // クライアント採番のシーケンス番号
	Payload   json.RawMessage `json:"payload"`
}

// ---------------------------------------------------------------------------
// ペイロード型
// ---------------------------------------------------------------------------

// Vector3 は位置・回転（spec-game.md §3.2）。
type Vector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// PlayerActorState はプレイヤーの行動状態（spec-game.md §3.2 の state）。
type PlayerActorState string

const (
	ActorIdle      PlayerActorState = "idle"
	ActorMoving    PlayerActorState = "moving"
	ActorAttacking PlayerActorState = "attacking"
	ActorDead      PlayerActorState = "dead"
)

// PlayerMovePayload は PLAYER_MOVE の payload。
type PlayerMovePayload struct {
	Position Vector3          `json:"position"`
	Rotation Vector3          `json:"rotation"`
	State    PlayerActorState `json:"state"`
}

// PlayerActionPayload は PLAYER_ACTION の payload。
type PlayerActionPayload struct {
	Action   string `json:"action"`             // attack / skill 等
	TargetID string `json:"targetId,omitempty"` // 対象プレイヤー ID
	Damage   int    `json:"damage,omitempty"`   // 与ダメージ（attack 時）
}

// PlayerState は SESSION_STATE に載る 1 プレイヤーのスナップショット（spec-game.md §3.2）。
type PlayerState struct {
	PlayerID string           `json:"playerId"`
	Position Vector3          `json:"position"`
	Rotation Vector3          `json:"rotation"`
	Health   int              `json:"health"` // 0〜100
	State    PlayerActorState `json:"state"`
}

// SessionStatePayload は SESSION_STATE の payload（全プレイヤーの最新状態）。
type SessionStatePayload struct {
	Players []PlayerState `json:"players"`
}

// SessionEndReason はセッション終了の理由。
type SessionEndReason string

const (
	EndCompleted SessionEndReason = "completed" // クリア/正常終了
	EndTimeout   SessionEndReason = "timeout"   // 最大時間（20 分）到達
	EndAbandoned SessionEndReason = "abandoned" // 全員離脱
)

// SessionEndPayload は SESSION_END の payload。
type SessionEndPayload struct {
	Reason SessionEndReason `json:"reason"`
}

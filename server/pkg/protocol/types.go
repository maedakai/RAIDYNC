// Package protocol は Lambda・マッチャー・DB 層が共有するデータ型を定義する。
// DynamoDB 項目と API 入出力の双方をここに集約し、層をまたぐ唯一の正典とする。
package protocol

// ---------------------------------------------------------------------------
// ドメイン定数
// ---------------------------------------------------------------------------

const (
	MaxPartySize     = 4  // 1 セッションの最大人数
	MinMatchSize     = 2  // 60 秒経過後に許容する最低人数（見切り発車）
	MaxWaitSeconds   = 60 // チケットの最大待機時間
	TicketTTLSeconds = 90 // DynamoDB TTL による自動削除までの猶予（待機 + マージン）
	PollIntervalSec  = 2  // クライアント推奨ポーリング間隔
)

// ---------------------------------------------------------------------------
// チケット状態
// ---------------------------------------------------------------------------

// TicketStatus はマッチメイキングチケットのライフサイクル状態。
// 遷移: WAITING → MATCHING → MATCHED / FAILED、WAITING → TIMEOUT / CANCELLED。
// FAILED は、揃って確保したが MATCHED を書き込めなかった（トランザクションの衝突が解けなかった）状態。
// TIMEOUT（60 秒待っても揃わなかった）とは原因が違うが、クライアントはどちらも match/request を出し直す。
type TicketStatus string

const (
	StatusWaiting   TicketStatus = "WAITING"   // 待機列に登録済み
	StatusMatching  TicketStatus = "MATCHING"  // マッチャーが確保（セッション生成中）
	StatusMatched   TicketStatus = "MATCHED"   // セッション確定、接続情報あり
	StatusTimeout   TicketStatus = "TIMEOUT"   // 60 秒経過で不成立
	StatusCancelled TicketStatus = "CANCELLED" // クライアントがキャンセル
	StatusFailed    TicketStatus = "FAILED"    // 確定の書き込みが衝突で失敗。出し直す
)

// ---------------------------------------------------------------------------
// DynamoDB 項目
// ---------------------------------------------------------------------------

// Ticket は MatchmakingTickets テーブルの 1 項目（PK: ticketId）。
type Ticket struct {
	TicketID         string          `dynamodbav:"ticketId"       json:"ticketId"`
	PlayerID         string          `dynamodbav:"playerId"       json:"playerId"`
	QuestID          string          `dynamodbav:"questId"        json:"questId"`
	PartyID          string          `dynamodbav:"partyId,omitempty" json:"partyId,omitempty"`
	Status           TicketStatus    `dynamodbav:"status"         json:"status"`
	Version          int             `dynamodbav:"version"        json:"version"` // 楽観ロック用
	Region           string          `dynamodbav:"region"         json:"region"`
	SoloAllowed      bool            `dynamodbav:"soloAllowed"    json:"soloAllowed"`
	LatencyHint      int             `dynamodbav:"latencyHint,omitempty" json:"latencyHint,omitempty"` // ms
	SessionID        string          `dynamodbav:"sessionId,omitempty"   json:"sessionId,omitempty"`
	ConnectionInfo   *ConnectionInfo `dynamodbav:"connectionInfo,omitempty" json:"connectionInfo,omitempty"`
	IdempotencyToken string          `dynamodbav:"idempotencyToken,omitempty" json:"-"`
	CreatedAt        int64           `dynamodbav:"createdAt"      json:"createdAt"`                      // UnixTime ms
	ExpiresAt        int64           `dynamodbav:"expiresAt"      json:"expiresAt"`                      // UnixTime ms
	MatchingAt       int64           `dynamodbav:"matchingAt,omitempty"     json:"matchingAt,omitempty"` // UnixTime ms
	TTL              int64           `dynamodbav:"ttl"            json:"-"`                              // UnixTime 秒（DynamoDB TTL 属性）
}

// Session は Sessions テーブルの 1 項目（PK: sessionId）。
type Session struct {
	SessionID      string         `dynamodbav:"sessionId"   json:"sessionId"`
	QuestID        string         `dynamodbav:"questId"     json:"questId"`
	PlayerIDs      []string       `dynamodbav:"playerIds"   json:"playerIds"`
	Status         SessionStatus  `dynamodbav:"status"      json:"status"`
	Region         string         `dynamodbav:"region"      json:"region"`
	ConnectionInfo ConnectionInfo `dynamodbav:"connectionInfo" json:"connectionInfo"`
	CreatedAt      int64          `dynamodbav:"createdAt"   json:"createdAt"` // UnixTime ms
	EndedAt        int64          `dynamodbav:"endedAt,omitempty" json:"endedAt,omitempty"`
}

// SessionStatus はゲームセッションの状態。
type SessionStatus string

const (
	SessionActive SessionStatus = "ACTIVE" // 進行中
	SessionEnded  SessionStatus = "ENDED"  // 終了済み
	SessionFailed SessionStatus = "FAILED" // マッチの確定に失敗し、片付けたセッション
)

// Player は Players テーブルの 1 項目（PK: playerId）。
type Player struct {
	PlayerID    string `dynamodbav:"playerId"    json:"playerId"`
	DisplayName string `dynamodbav:"displayName" json:"displayName"`
	CreatedAt   int64  `dynamodbav:"createdAt"   json:"createdAt"` // UnixTime ms
}

// MatchHistoryEntry は MatchHistory テーブルの 1 項目（PK: playerId, SK: timestamp）。
type MatchHistoryEntry struct {
	PlayerID    string      `dynamodbav:"playerId"  json:"playerId"`
	Timestamp   int64       `dynamodbav:"timestamp" json:"timestamp"` // UnixTime ms（SK）
	SessionID   string      `dynamodbav:"sessionId" json:"sessionId"`
	QuestID     string      `dynamodbav:"questId"   json:"questId"`
	Result      MatchResult `dynamodbav:"result"    json:"result"`
	DurationSec int         `dynamodbav:"durationSec" json:"durationSec"`
}

// MatchResult はマッチ 1 回の結末。
type MatchResult string

const (
	ResultClear     MatchResult = "CLEAR"     // クリア
	ResultFailed    MatchResult = "FAILED"    // 失敗
	ResultAbandoned MatchResult = "ABANDONED" // 途中離脱
)

// ConnectionInfo は GameLift から得る接続先（Phase 2 はスタブが固定値を返す）。
type ConnectionInfo struct {
	IPAddress       string `dynamodbav:"ipAddress"       json:"ipAddress"`
	Port            int    `dynamodbav:"port"            json:"port"`
	PlayerSessionID string `dynamodbav:"playerSessionId" json:"playerSessionId"`
}

// ---------------------------------------------------------------------------
// API 入出力（JSON のみ。DynamoDB には保存しない）
// ---------------------------------------------------------------------------

// MatchRequestInput は POST /match/request のリクエストボディ。
type MatchRequestInput struct {
	QuestID     string `json:"questId"`
	SoloAllowed bool   `json:"soloAllowed,omitempty"`
	PartyID     string `json:"partyId,omitempty"`
	LatencyHint int    `json:"latencyHint,omitempty"` // ms
}

// MatchRequestOutput は POST /match/request のレスポンス。
//
// Endpoint / Region はマルチリージョン構成でのセッションアフィニティ用。
// チケットは region-local なので、以降の match/status・match/cancel は
// このチケットを作ったリージョンへ送らなければ 404 になる。クライアントは
// Endpoint が返ってきたら、以降のポーリングをそちらへ向けること。
type MatchRequestOutput struct {
	TicketID  string       `json:"ticketId"`
	Status    TicketStatus `json:"status"`
	CreatedAt int64        `json:"createdAt"` // UnixTime ms
	ExpiresAt int64        `json:"expiresAt"` // UnixTime ms
	Endpoint  string       `json:"endpoint,omitempty"`
	Region    string       `json:"region,omitempty"`
}

// MatchStatusOutput は GET /match/status/{ticketId} のレスポンス。
// status により有効なフィールドが変わる（WAITING のみ waitedSec、MATCHED のみ接続情報）。
type MatchStatusOutput struct {
	TicketID       string          `json:"ticketId"`
	Status         TicketStatus    `json:"status"`
	WaitedSec      int             `json:"waitedSec,omitempty"`
	SessionID      string          `json:"sessionId,omitempty"`
	ConnectionInfo *ConnectionInfo `json:"connectionInfo,omitempty"`
}

// MatchHistoryOutput は GET /player/{playerId}/history のレスポンス。
type MatchHistoryOutput struct {
	PlayerID string              `json:"playerId"`
	Entries  []MatchHistoryEntry `json:"entries"`
}

// SessionEndInput は POST /session/{sessionId}/end のリクエストボディ。
// ゲームサーバ（本番では GameLift プロセス）がセッション終了時に結果を報告する。
type SessionEndInput struct {
	Result      MatchResult `json:"result"`
	DurationSec int         `json:"durationSec"`
}

// SessionEndOutput は結果保存の要約。playersSaved は履歴を書いた人数。
type SessionEndOutput struct {
	SessionID    string        `json:"sessionId"`
	Status       SessionStatus `json:"status"`
	PlayersSaved int           `json:"playersSaved"`
}

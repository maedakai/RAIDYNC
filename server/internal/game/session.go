package game

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"

	"github.com/raidync/server/pkg/protocol"
)

// maxSessionMs は 1 セッションの最大継続時間（ms）。
const maxSessionMs = protocol.MaxSessionMinutes * 60 * 1000

// PlayerObserver はセッションへのプレイヤー参加/離脱を受け取る。GameLift Bridge が
// AcceptPlayerSession(psid) / RemovePlayerSession(psid) を呼ぶために実装する。
// 各コールバックは Session の Mutex を保持していない状態で呼ばれる（実装側が
// ネットワーク I/O を行っても受信/ブロードキャストのホットパスを止めない）。
// ローカル検証では注入しない（nil=通知しない）ので既存の E2E 挙動は不変。
type PlayerObserver interface {
	OnPlayerJoin(sessionID, playerID, playerSessionID string)
	OnPlayerLeave(sessionID, playerID, playerSessionID string)
}

// SessionOption は NewSession の任意設定。既存呼び出し（オプション無し）は挙動不変。
type SessionOption func(*Session)

// WithPlayerObserver は参加/離脱の通知先を設定する（GameLift モードで Bridge を渡す）。
func WithPlayerObserver(obs PlayerObserver) SessionOption {
	return func(s *Session) { s.observer = obs }
}

// WithPlayerSessions は playerId→playerSessionId の対応表を設定する。
// GameLift の Accept/RemovePlayerSession は playerSessionId を要求するため、roster(playerId)
// とこの表で橋渡しする。ローカルは空（psid 不明時は Bridge 側が SDK 呼び出しをスキップ）。
func WithPlayerSessions(m map[string]string) SessionOption {
	return func(s *Session) { s.playerSessions = m }
}

// playerRef は参加/離脱通知に必要な最小情報（ロック外へ持ち出す用）。
type playerRef struct {
	playerID        string
	playerSessionID string
}

// Session は 1 ゲームセッション（最大 4 人）の実行時状態を保持する。
// 受信メッセージの取り込み（Ingest）とブロードキャスト用スナップショット生成（Snapshot）が
// 主な責務。送信そのものは行わず、宛先（*net.UDPAddr）の一覧だけを呼び出し側に渡す。
//
// すべての公開メソッドはゴルーチンセーフ（内部 Mutex で保護）。
type Session struct {
	ID      string
	QuestID string

	mu             sync.Mutex
	roster         map[string]struct{} // マッチメイキングで確定した参加許可 playerId 集合
	players        map[string]*player
	playerSessions map[string]string // playerId → playerSessionId（GameLift 契約用。ローカルは空）
	outSeq         uint32            // SESSION_STATE の送信シーケンス（サーバー採番）
	createdAt      int64             // UnixTime ms
	endsAt         int64             // createdAt + 20 分。これを過ぎたら timeout 終了
	ended          bool

	observer PlayerObserver // 参加/離脱通知先。nil=通知しない（ローカル既定）
}

// NewSession は roster（参加許可プレイヤー）を持つ新規セッションを生成する。
// roster は Lambda 側のマッチメイキング結果（protocol.Session.PlayerIDs）と一致させる。
func NewSession(id, questID string, roster []string, now int64, opts ...SessionOption) *Session {
	set := make(map[string]struct{}, len(roster))
	for _, pid := range roster {
		set[pid] = struct{}{}
	}
	s := &Session{
		ID:             id,
		QuestID:        questID,
		roster:         set,
		players:        make(map[string]*player, len(roster)),
		playerSessions: make(map[string]string, len(roster)),
		createdAt:      now,
		endsAt:         now + maxSessionMs,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Ingest は受信した 1 メッセージをセッション状態へ反映する。
//   - roster 外の playerId は拒否（ErrNotInRoster）
//   - seq が受理済み最大値以下なら古い/重複として黙って破棄（nil を返す）
//   - 正常に処理できた場合は送信元 addr と lastSeen を更新する
func (s *Session) Ingest(msg *protocol.GameMessage, addr *net.UDPAddr, now int64) error {
	// 参加通知はロック解放後に行う。この defer を先に積む＝LIFO で s.mu.Unlock() の後に走る。
	var joined *playerRef
	defer func() {
		if joined != nil && s.observer != nil {
			s.observer.OnPlayerJoin(s.ID, joined.playerID, joined.playerSessionID)
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return ErrSessionEnded
	}
	if _, ok := s.roster[msg.PlayerID]; !ok {
		return fmt.Errorf("%w: %s", ErrNotInRoster, msg.PlayerID)
	}

	p := s.players[msg.PlayerID]
	if p == nil {
		p = newPlayer(msg.PlayerID, now)
		s.players[msg.PlayerID] = p
		joined = &playerRef{playerID: msg.PlayerID, playerSessionID: s.playerSessions[msg.PlayerID]}
	}

	// シーケンスゲート: UDP は順序保証がないため、古い/重複パケットを捨てる。
	if p.seen && msg.Seq <= p.lastSeq {
		return nil
	}
	p.seen = true
	p.lastSeq = msg.Seq
	p.addr = addr
	p.lastSeen = now

	switch msg.Type {
	case protocol.MsgPlayerMove:
		var pl protocol.PlayerMovePayload
		if err := json.Unmarshal(msg.Payload, &pl); err != nil {
			return fmt.Errorf("unmarshal PLAYER_MOVE: %w", err)
		}
		if p.state != protocol.ActorDead { // 死亡後の移動は無視
			p.position = pl.Position
			p.rotation = pl.Rotation
			p.state = pl.State
		}
	case protocol.MsgPlayerAction:
		var pl protocol.PlayerActionPayload
		if err := json.Unmarshal(msg.Payload, &pl); err != nil {
			return fmt.Errorf("unmarshal PLAYER_ACTION: %w", err)
		}
		s.applyAction(p, pl)
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedInbound, msg.Type)
	}
	return nil
}

// applyAction は PLAYER_ACTION を反映する（mu を保持した状態で呼ぶこと）。
// 現状は attack による対象プレイヤーへのダメージのみ扱う（敵 AI は Phase 3 スコープ外）。
func (s *Session) applyAction(actor *player, pl protocol.PlayerActionPayload) {
	if actor.state != protocol.ActorDead {
		actor.state = protocol.ActorAttacking
	}
	if pl.TargetID == "" || pl.Damage <= 0 {
		return
	}
	if target := s.players[pl.TargetID]; target != nil {
		target.applyDamage(pl.Damage)
	}
}

// Snapshot は現在の全プレイヤー状態を SESSION_STATE メッセージとして組み立て、
// 送信先 addr の一覧とともに返す。送信シーケンスはここで採番する。
// プレイヤーがまだ 1 人も接続していない場合は ok=false。
func (s *Session) Snapshot(now int64) (msg protocol.GameMessage, recipients []*net.UDPAddr, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.players) == 0 {
		return protocol.GameMessage{}, nil, false
	}

	states := make([]protocol.PlayerState, 0, len(s.players))
	recipients = make([]*net.UDPAddr, 0, len(s.players))
	for _, p := range s.players {
		states = append(states, p.snapshot())
		if p.addr != nil {
			recipients = append(recipients, p.addr)
		}
	}

	s.outSeq++
	payload, _ := json.Marshal(protocol.SessionStatePayload{Players: states})
	msg = protocol.GameMessage{
		Type:      protocol.MsgSessionState,
		SessionID: s.ID,
		Timestamp: now,
		Seq:       s.outSeq,
		Payload:   payload,
	}
	return msg, recipients, true
}

// PruneIdle は最終受信から PlayerIdleTimeoutMs を超えたプレイヤーを取り除く。
// 取り除いた人数を返す。
func (s *Session) PruneIdle(now int64) int {
	// 離脱通知はロック解放後に行う（LIFO で s.mu.Unlock() の後に走る）。
	var left []playerRef
	defer func() {
		if s.observer != nil {
			for _, r := range left {
				s.observer.OnPlayerLeave(s.ID, r.playerID, r.playerSessionID)
			}
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	removed := 0
	for id, p := range s.players {
		if now-p.lastSeen > protocol.PlayerIdleTimeoutMs {
			delete(s.players, id)
			left = append(left, playerRef{playerID: id, playerSessionID: s.playerSessions[id]})
			removed++
		}
	}
	return removed
}

// EndReason はセッションを終了すべきか判定し、終了理由を返す。
//   - 最大時間（20 分）到達 → timeout
//   - 一度でも接続したが全員離脱 → abandoned
//
// まだ終了すべきでなければ ok=false。
func (s *Session) EndReason(now int64) (protocol.SessionEndReason, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return "", false
	}
	if now >= s.endsAt {
		return protocol.EndTimeout, true
	}
	return "", false
}

// EndMessage はセッションを終了状態にし、SESSION_END メッセージと送信先を返す。
// 多重呼び出しは ok=false（冪等）。
func (s *Session) EndMessage(reason protocol.SessionEndReason, now int64) (msg protocol.GameMessage, recipients []*net.UDPAddr, ok bool) {
	// セッション終了時、接続中の全員を離脱扱いにして枠を返す（ロック解放後に通知）。
	var left []playerRef
	defer func() {
		if s.observer != nil {
			for _, r := range left {
				s.observer.OnPlayerLeave(s.ID, r.playerID, r.playerSessionID)
			}
		}
	}()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ended {
		return protocol.GameMessage{}, nil, false
	}
	s.ended = true

	recipients = make([]*net.UDPAddr, 0, len(s.players))
	for id, p := range s.players {
		if p.addr != nil {
			recipients = append(recipients, p.addr)
		}
		left = append(left, playerRef{playerID: id, playerSessionID: s.playerSessions[id]})
	}
	s.outSeq++
	payload, _ := json.Marshal(protocol.SessionEndPayload{Reason: reason})
	msg = protocol.GameMessage{
		Type:      protocol.MsgSessionEnd,
		SessionID: s.ID,
		Timestamp: now,
		Seq:       s.outSeq,
		Payload:   payload,
	}
	return msg, recipients, true
}

// PlayerCount は現在接続中のプレイヤー数を返す。
func (s *Session) PlayerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.players)
}

// Ended はセッションが終了済みかを返す。
func (s *Session) Ended() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ended
}

package game

import (
	"encoding/json"
	"net"
	"sync"

	"github.com/raidync/server/pkg/protocol"
)

// Sender は 1 つの UDP データグラムを宛先へ送る抽象。
// 本番は *net.UDPConn を包んだ実装が、テストはフェイクが満たす（net 依存をサーバー層に閉じ込める）。
type Sender interface {
	SendTo(addr *net.UDPAddr, data []byte) error
}

// SessionEndObserver はセッションが終わったことを受け取る。報告係が /session/end を呼ぶために実装する。
// OnSessionEnd は 1 セッションにつき 1 回だけ、Hub の Mutex を保持していない状態で呼ばれる。
// Tick（50ms ごとに全セッションを処理する）の中から呼ばれるため、実装は戻りを待たせないこと
// （ネットワーク I/O は別のゴルーチンで行う）。ローカル検証では注入しない（nil=通知しない）。
type SessionEndObserver interface {
	OnSessionEnd(sessionID string, reason protocol.SessionEndReason, durationSec int)
}

// HubOption は NewHub の任意設定。既存呼び出し（オプション無し）は挙動不変。
type HubOption func(*Hub)

// WithSessionEndObserver はセッション終了の通知先を設定する。
func WithSessionEndObserver(obs SessionEndObserver) HubOption {
	return func(h *Hub) { h.endObserver = obs }
}

// Hub は同一プロセス上の複数セッションを束ね、受信ルーティングと
// 定期ブロードキャスト（SESSION_STATE 20Hz、ライフサイクル管理）を担う。
// GameLift の 1 サーバープロセス = 1 セッションが基本だが、ローカル検証では
// 複数セッションを同居させられるよう汎用化しておく。
type Hub struct {
	sender Sender

	mu       sync.RWMutex
	sessions map[string]*Session

	endObserver SessionEndObserver // セッション終了の通知先。nil=通知しない（ローカル既定）
}

// NewHub は送信手段 sender を注入して Hub を生成する。
func NewHub(sender Sender, opts ...HubOption) *Hub {
	h := &Hub{sender: sender, sessions: make(map[string]*Session)}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Register はセッションを登録する。
func (h *Hub) Register(s *Session) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions[s.ID] = s
}

// session は id でセッションを取得する（読み取りロック）。
func (h *Hub) session(id string) *Session {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.sessions[id]
}

// Route は受信した 1 メッセージを sessionId に対応するセッションへ取り込む。
// 未知の sessionId は ErrSessionNotFound。
func (h *Hub) Route(msg *protocol.GameMessage, addr *net.UDPAddr, now int64) error {
	s := h.session(msg.SessionID)
	if s == nil {
		return ErrSessionNotFound
	}
	return s.Ingest(msg, addr, now)
}

// Tick は 1 周期分の処理を行う。SESSION_STATE のブロードキャスト周期（50ms）ごとに呼ぶ想定。
//   - 各セッションの SESSION_STATE を全プレイヤーへ送信
//   - アイドルプレイヤーの除去
//   - 終了条件（EndReason）を満たしたセッションに SESSION_END を送って登録解除し、終了を通知
func (h *Hub) Tick(now int64) {
	h.mu.RLock()
	sessions := make([]*Session, 0, len(h.sessions))
	for _, s := range h.sessions {
		sessions = append(sessions, s)
	}
	h.mu.RUnlock()

	for _, s := range sessions {
		s.PruneIdle(now)

		if reason, ok := s.EndReason(now); ok {
			h.endSession(s, reason, now)
			continue
		}

		if msg, recipients, ok := s.Snapshot(now); ok {
			h.broadcast(msg, recipients)
		}
	}
}

// endSession は SESSION_END を送出してセッションを登録解除し、終了を通知する。
// Tick と End が重なっても、送出と通知は EndMessage が ok を返した 1 回だけ行う。
func (h *Hub) endSession(s *Session, reason protocol.SessionEndReason, now int64) {
	msg, recipients, ok := s.EndMessage(reason, now)
	if ok {
		h.broadcast(msg, recipients)
	}
	h.mu.Lock()
	delete(h.sessions, s.ID)
	h.mu.Unlock()

	if ok && h.endObserver != nil {
		h.endObserver.OnSessionEnd(s.ID, reason, s.Duration(now))
	}
}

// broadcast は 1 メッセージを 1 回だけマーシャルし、全宛先へ送る。
func (h *Hub) broadcast(msg protocol.GameMessage, recipients []*net.UDPAddr) {
	if len(recipients) == 0 {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	for _, addr := range recipients {
		_ = h.sender.SendTo(addr, data) // UDP は送りっぱなし。個別失敗は無視
	}
}

// SessionCount は登録中のセッション数を返す。
func (h *Hub) SessionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// End は外から指定されたセッションを、時間切れを待たずに終わらせる（理由は EndAbandoned）。
// マッチの確定に失敗したセッションを片付けるために使う。登録されていない id では何もしない。
func (h *Hub) End(id string, now int64) {
	s := h.session(id)
	if s == nil {
		return
	}
	h.endSession(s, protocol.EndAbandoned, now)
}

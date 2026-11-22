package matchmaking

import (
	"context"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

// fakeStore はテスト用のインメモリ Store 実装。
// DynamoDB の楽観ロック挙動（version + status 条件）を再現し、Lock の競合や TIMEOUT 競合を検証可能にする。
type fakeStore struct {
	tickets        map[string]*protocol.Ticket
	sessions       map[string]*protocol.Session
	claimErr       error // 設定時は ClaimTickets が常にこのエラーを返す（競合シミュレーション）
	timeoutErr     error
	markMatchedErr error  // 設定時は MarkMatched が常にこのエラーを返す（送り直しても衝突した場合のシミュレーション）
	failSessionErr error  // 設定時は FailSession が常にこのエラーを返す（Sessions の更新が失敗した場合のシミュレーション）
	markFailedErr  error  // 設定時は MarkFailed が常にこのエラーを返す（状態が分からない失敗のシミュレーション）
	onMarkMatched  func() // 設定時は MarkMatched が markMatchedErr を返す直前に呼ぶ（書き込みが実は通っていた・救済で先に FAILED にされた場面を作る）
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		tickets:  map[string]*protocol.Ticket{},
		sessions: map[string]*protocol.Session{},
	}
}

// put はテストアレンジ用にチケットを直接投入する。
func (f *fakeStore) put(t *protocol.Ticket) {
	f.tickets[t.TicketID] = t
}

func (f *fakeStore) GetTicket(_ context.Context, ticketID string) (*protocol.Ticket, error) {
	t, ok := f.tickets[ticketID]
	if !ok {
		return nil, nil
	}
	cp := *t // 防御的コピー
	return &cp, nil
}

func (f *fakeStore) QueryWaitingByQuest(_ context.Context, questID string, _ int32) ([]protocol.Ticket, error) {
	var out []protocol.Ticket
	for _, t := range f.tickets {
		if t.QuestID == questID && t.Status == protocol.StatusWaiting {
			out = append(out, *t)
		}
	}
	return out, nil
}

func (f *fakeStore) ClaimTickets(_ context.Context, tickets []protocol.Ticket, matchingAt int64, idempotencyToken string) error {
	if f.claimErr != nil {
		return f.claimErr
	}
	// 条件チェック（全件 WAITING かつ version 一致）
	for _, t := range tickets {
		cur, ok := f.tickets[t.TicketID]
		if !ok || cur.Status != protocol.StatusWaiting || cur.Version != t.Version {
			return db.ErrConflict
		}
	}
	// 適用
	for _, t := range tickets {
		cur := f.tickets[t.TicketID]
		cur.Status = protocol.StatusMatching
		cur.Version = t.Version + 1
		cur.MatchingAt = matchingAt
		cur.IdempotencyToken = idempotencyToken
	}
	return nil
}

func (f *fakeStore) MarkMatched(_ context.Context, tickets []protocol.Ticket, sessionID string, conn protocol.ConnectionInfo, playerSessions map[string]string) error {
	if f.markMatchedErr != nil {
		if f.onMarkMatched != nil {
			f.onMarkMatched()
		}
		return f.markMatchedErr
	}
	for _, t := range tickets {
		cur, ok := f.tickets[t.TicketID]
		if !ok || cur.Status != protocol.StatusMatching || cur.Version != t.Version {
			return db.ErrConflict
		}
		cur.Status = protocol.StatusMatched
		cur.SessionID = sessionID
		cur.Version = t.Version + 1
		connCopy := conn
		connCopy.PlayerSessionID = playerSessions[t.PlayerID]
		cur.ConnectionInfo = &connCopy
	}
	return nil
}

func (f *fakeStore) MarkFailed(_ context.Context, tickets []protocol.Ticket) error {
	if f.markFailedErr != nil {
		return f.markFailedErr
	}
	for _, t := range tickets {
		cur, ok := f.tickets[t.TicketID]
		if !ok || cur.Status != protocol.StatusMatching || cur.Version != t.Version {
			return db.ErrConflict
		}
		cur.Status = protocol.StatusFailed
		cur.Version = t.Version + 1
	}
	return nil
}

func (f *fakeStore) PutSession(_ context.Context, s protocol.Session) error {
	sc := s
	f.sessions[s.SessionID] = &sc
	return nil
}

func (f *fakeStore) FailSession(_ context.Context, sessionID string, _ int64) error {
	if f.failSessionErr != nil {
		return f.failSessionErr
	}
	if s, ok := f.sessions[sessionID]; ok {
		s.Status = protocol.SessionFailed
	}
	return nil
}

func (f *fakeStore) TimeoutTicket(_ context.Context, ticketID string) error {
	if f.timeoutErr != nil {
		return f.timeoutErr
	}
	cur, ok := f.tickets[ticketID]
	if !ok || cur.Status != protocol.StatusWaiting {
		return db.ErrConflict
	}
	cur.Status = protocol.StatusTimeout
	return nil
}

// fakeSessionProvider は受け取った sessionId をそのまま返し、接続情報は固定の値を返す。
// psid はプレイヤー毎に "ps-<playerID>" を採番する（本番の 1 プレイヤー 1 枠を模す）。
type fakeSessionProvider struct {
	conn     protocol.ConnectionInfo
	calls    int
	endedIDs []string // EndGameSession が呼ばれた sessionID の記録
	endErr   error    // 設定時は EndGameSession がこのエラーを返す（記録はしてから返す）
}

func (f *fakeSessionProvider) CreateGameSession(_ context.Context, sessionID, _ string, playerIDs []string) (string, protocol.ConnectionInfo, map[string]string, error) {
	f.calls++
	playerSessions := make(map[string]string, len(playerIDs))
	for _, pid := range playerIDs {
		playerSessions[pid] = "ps-" + pid
	}
	return sessionID, f.conn, playerSessions, nil
}

func (f *fakeSessionProvider) EndGameSession(_ context.Context, sessionID string) error {
	f.endedIDs = append(f.endedIDs, sessionID)
	return f.endErr
}

func (f *fakeStore) FindMatchingByToken(ctx context.Context, questID, token string) ([]protocol.Ticket, error) {
	var out []protocol.Ticket
	for _, t := range f.tickets {
		if t.QuestID == questID && t.Status == protocol.StatusMatching && t.IdempotencyToken == token {
			out = append(out, *t)
		}
	}
	return out, nil
}

func newFakeSessionProvider() *fakeSessionProvider {
	return &fakeSessionProvider{
		conn: protocol.ConnectionInfo{IPAddress: "10.0.0.1", Port: 7777},
	}
}

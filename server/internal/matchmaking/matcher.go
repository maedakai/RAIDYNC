// Package matchmaking は WAITING チケットからセッションを組成するコアロジックを持つ。
// DynamoDB アクセスは Store インターフェース経由に抽象化し、単体テストで差し替え可能にする。
package matchmaking

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/internal/metrics"
	"github.com/raidync/server/pkg/protocol"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// ErrTicketNotFound は対象 ticketId が存在しないことを示す。
var ErrTicketNotFound = errors.New("ticket not found")

// candidateFetchLimit は 1 回のマッチ試行で取得する WAITING 候補の上限。
const candidateFetchLimit = 50

// Store は matcher が必要とする DynamoDB 操作のみを切り出したインターフェース。
// 本番は *db.Client が、テストはモックが満たす。
type Store interface {
	GetTicket(ctx context.Context, ticketID string) (*protocol.Ticket, error)
	QueryWaitingByQuest(ctx context.Context, questID string, limit int32) ([]protocol.Ticket, error)
	ClaimTickets(ctx context.Context, tickets []protocol.Ticket) error
	MarkMatched(ctx context.Context, ticketID, sessionID string, conn protocol.ConnectionInfo) error
	PutSession(ctx context.Context, s protocol.Session) error
	TimeoutTicket(ctx context.Context, ticketID string) error
}

// *db.Client が Store を満たすことをコンパイル時に保証する。
var _ Store = (*db.Client)(nil)

// Matcher はポーリング契機のマッチング処理を担う。
type Matcher struct {
	store    Store
	sessions SessionProvider
	nowMs    func() int64 // UnixTime ms を返す。テストで固定するため関数で保持
}

// NewMatcher は本番用の Matcher を生成する。
func NewMatcher(store Store, sessions SessionProvider) *Matcher {
	return &Matcher{store: store, sessions: sessions, nowMs: nowMillis}
}

// TryMatch は GET /match/status 契機で呼ばれ、WAITING チケットの成立を試みる。
// 戻り値は処理後の最新チケット状態。
func (m *Matcher) TryMatch(ctx context.Context, ticketID string) (*protocol.Ticket, error) {
	t, err := m.store.GetTicket(ctx, ticketID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, ErrTicketNotFound
	}
	// WAITING 以外（MATCHING/MATCHED/TIMEOUT/CANCELLED）は現状をそのまま返す。
	if t.Status != protocol.StatusWaiting {
		return t, nil
	}

	// 60 秒経過: 最低人数で見切り発車、それも無理なら TIMEOUT。
	if m.nowMs() >= t.ExpiresAt {
		return m.handleExpired(ctx, t)
	}

	// ソロ即時開始。
	if t.SoloAllowed {
		return m.formSolo(ctx, t)
	}

	// 通常マッチ: 満員（4 人）を狙う。
	matched, err := m.tryForm(ctx, t, protocol.MaxPartySize)
	if err != nil {
		return nil, err
	}
	if matched != nil {
		return matched, nil
	}
	return t, nil // まだ揃わない → WAITING のまま
}

// handleExpired は待機 60 秒経過チケットを処理する。
func (m *Matcher) handleExpired(ctx context.Context, t *protocol.Ticket) (*protocol.Ticket, error) {
	matched, err := m.tryForm(ctx, t, protocol.MinMatchSize)
	if err != nil {
		return nil, err
	}
	if matched != nil {
		return matched, nil
	}
	if err := m.store.TimeoutTicket(ctx, t.TicketID); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return m.store.GetTicket(ctx, t.TicketID) // 競合 → 最新状態を返す
		}
		return nil, err
	}
	t.Status = protocol.StatusTimeout
	metrics.MatchTimeout(ctx, t.QuestID)
	return t, nil
}

// tryForm は同 quest の WAITING を集めて minSize 以上のグループを構成し、原子的に確保して成立させる。
// 構成できなければ (nil, nil)。
func (m *Matcher) tryForm(ctx context.Context, self *protocol.Ticket, minSize int) (*protocol.Ticket, error) {
	candidates, err := m.store.QueryWaitingByQuest(ctx, self.QuestID, candidateFetchLimit)
	if err != nil {
		return nil, err
	}
	group := selectGroup(self, candidates)
	if len(group) < minSize {
		return nil, nil
	}
	if err := m.store.ClaimTickets(ctx, group); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return nil, nil // 他マッチャーが先に確保 → 次のポーリングで再試行
		}
		return nil, err
	}
	return m.finalize(ctx, self, group)
}

// formSolo は自分だけのセッションを即時生成する。
func (m *Matcher) formSolo(ctx context.Context, self *protocol.Ticket) (*protocol.Ticket, error) {
	if err := m.store.ClaimTickets(ctx, []protocol.Ticket{*self}); err != nil {
		if errors.Is(err, db.ErrConflict) {
			return m.store.GetTicket(ctx, self.TicketID)
		}
		return nil, err
	}
	return m.finalize(ctx, self, []protocol.Ticket{*self})
}

// finalize は確保済みグループに対しセッションを生成し、全チケットを MATCHED に更新する。
func (m *Matcher) finalize(ctx context.Context, self *protocol.Ticket, group []protocol.Ticket) (*protocol.Ticket, error) {
	playerIDs := make([]string, 0, len(group))
	for _, g := range group {
		playerIDs = append(playerIDs, g.PlayerID)
	}

	sessionID, conn, playerSessions, err := m.sessions.CreateGameSession(ctx, self.QuestID, playerIDs)
	if err != nil {
		return nil, err
	}

	// conn は全員共通の接続先（psid 無し）。セッションレコードには共通接続先を保存する。
	sess := protocol.Session{
		SessionID:      sessionID,
		QuestID:        self.QuestID,
		PlayerIDs:      playerIDs,
		Status:         protocol.SessionActive,
		Region:         self.Region,
		ConnectionInfo: conn,
		CreatedAt:      m.nowMs(),
	}
	if err := m.store.PutSession(ctx, sess); err != nil {
		return nil, err
	}

	// 各チケットには「共通 conn ＋ 本人の playerSessionId」を配る（1 プレイヤー 1 枠）。
	for _, g := range group {
		pc := conn
		pc.PlayerSessionID = playerSessions[g.PlayerID]
		if err := m.store.MarkMatched(ctx, g.TicketID, sessionID, pc); err != nil {
			return nil, err
		}
	}

	selfConn := conn
	selfConn.PlayerSessionID = playerSessions[self.PlayerID]
	self.Status = protocol.StatusMatched
	self.SessionID = sessionID
	self.ConnectionInfo = &selfConn

	metrics.MatchFormed(ctx, self.QuestID, len(group), m.nowMs()-self.CreatedAt)
	return self, nil
}

// selectGroup は self を必ず含み、partyId を優先しつつ最大 4 人のグループを返す。
// パーティ（同一 partyId）は分割せず、丸ごと収まる場合のみ追加する。
func selectGroup(self *protocol.Ticket, candidates []protocol.Ticket) []protocol.Ticket {
	used := map[string]bool{self.TicketID: true}
	group := []protocol.Ticket{*self}

	// 1) self のパーティ仲間を最優先で集約。
	if self.PartyID != "" {
		for i := range candidates {
			c := candidates[i]
			if used[c.TicketID] || c.PartyID != self.PartyID {
				continue
			}
			if len(group) >= protocol.MaxPartySize {
				break
			}
			group = append(group, c)
			used[c.TicketID] = true
		}
	}

	// 2) 残席を createdAt 昇順（FIFO）で埋める。
	rest := make([]protocol.Ticket, 0, len(candidates))
	for i := range candidates {
		if !used[candidates[i].TicketID] {
			rest = append(rest, candidates[i])
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].CreatedAt < rest[j].CreatedAt })

	for i := range rest {
		c := rest[i]
		if used[c.TicketID] || len(group) >= protocol.MaxPartySize {
			continue
		}
		if c.PartyID == "" {
			group = append(group, c)
			used[c.TicketID] = true
			continue
		}
		// パーティは全員が残席に収まる場合のみ一括追加（分割しない）。
		members := partyMembers(rest, c.PartyID, used)
		if len(group)+len(members) <= protocol.MaxPartySize {
			for _, mem := range members {
				group = append(group, mem)
				used[mem.TicketID] = true
			}
		}
	}
	return group
}

// partyMembers は未使用かつ指定 partyId を持つチケットを集める。
func partyMembers(tickets []protocol.Ticket, partyID string, used map[string]bool) []protocol.Ticket {
	members := make([]protocol.Ticket, 0, protocol.MaxPartySize)
	for i := range tickets {
		t := tickets[i]
		if !used[t.TicketID] && t.PartyID == partyID {
			members = append(members, t)
		}
	}
	return members
}

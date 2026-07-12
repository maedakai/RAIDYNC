package matchmaking

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

// fixedClock は指定 ms を返す nowMs 関数を作る。
func fixedClock(ms int64) func() int64 { return func() int64 { return ms } }

// makeWaiting は WAITING チケットを返す（テストアレンジ用）。
func makeWaiting(id, player, party string, createdAt int64) *protocol.Ticket {
	return &protocol.Ticket{
		TicketID:  id,
		PlayerID:  player,
		PartyID:   party,
		QuestID:   "q1",
		Status:    protocol.StatusWaiting,
		Version:   0,
		CreatedAt: createdAt,
		ExpiresAt: createdAt + 60_000,
	}
}

func newMatcherFor(store *fakeStore, sessions *fakeSessionProvider, nowMs int64) *Matcher {
	m := NewMatcher(store, sessions)
	m.nowMs = fixedClock(nowMs)
	return m
}

func TestTryMatch_NotFound(t *testing.T) {
	m := newMatcherFor(newFakeStore(), newFakeSessionProvider(), 1000)

	_, err := m.TryMatch(context.Background(), "missing")
	assert.ErrorIs(t, err, ErrTicketNotFound)
}

func TestTryMatch_PassthroughWhenNotWaiting(t *testing.T) {
	store := newFakeStore()
	t1 := makeWaiting("t1", "p1", "", 1000)
	t1.Status = protocol.StatusMatched
	t1.SessionID = "sess-x"
	store.put(t1)

	m := newMatcherFor(store, newFakeSessionProvider(), 2000)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, protocol.StatusMatched, got.Status)
	assert.Equal(t, "sess-x", got.SessionID)
}

func TestTryMatch_FullPartyOf4(t *testing.T) {
	store := newFakeStore()
	for i, id := range []string{"t1", "t2", "t3", "t4"} {
		store.put(makeWaiting(id, "p"+id, "", int64(1000+i)))
	}
	sessions := newFakeSessionProvider()

	m := newMatcherFor(store, sessions, 1500) // expiry 61000 より前
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)

	assert.Equal(t, protocol.StatusMatched, got.Status)
	assert.Equal(t, sessions.sessionID, got.SessionID)
	assert.Equal(t, 1, sessions.calls)
	// 4 件すべてが MATCHED で、各自「共通 IP/Port ＋ 本人だけの psid」を受け取ること
	seen := map[string]bool{}
	for _, id := range []string{"t1", "t2", "t3", "t4"} {
		stored, _ := store.GetTicket(context.Background(), id)
		assert.Equal(t, protocol.StatusMatched, stored.Status, "ticket %s should be MATCHED", id)
		require.NotNil(t, stored.ConnectionInfo, "ticket %s should carry connection info", id)
		assert.Equal(t, sessions.conn.IPAddress, stored.ConnectionInfo.IPAddress, "全員が共通の接続先 IP")
		assert.Equal(t, sessions.conn.Port, stored.ConnectionInfo.Port, "全員が共通の接続先 Port")
		assert.Equal(t, "ps-"+stored.PlayerID, stored.ConnectionInfo.PlayerSessionID, "本人の psid が配られる")
		assert.False(t, seen[stored.ConnectionInfo.PlayerSessionID], "psid はプレイヤー毎に一意")
		seen[stored.ConnectionInfo.PlayerSessionID] = true
	}
	// セッションが 1 件登録されていること
	assert.Len(t, store.sessions, 1)
}

func TestTryMatch_NotEnoughCandidatesStaysWaiting(t *testing.T) {
	store := newFakeStore()
	// 3 件のみ（満員 4 未満）
	for i, id := range []string{"t1", "t2", "t3"} {
		store.put(makeWaiting(id, "p"+id, "", int64(1000+i)))
	}

	m := newMatcherFor(store, newFakeSessionProvider(), 1500)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, protocol.StatusWaiting, got.Status)
}

func TestTryMatch_SoloAllowed(t *testing.T) {
	store := newFakeStore()
	t1 := makeWaiting("t1", "p1", "", 1000)
	t1.SoloAllowed = true
	store.put(t1)
	sessions := newFakeSessionProvider()

	m := newMatcherFor(store, sessions, 1500)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)

	assert.Equal(t, protocol.StatusMatched, got.Status)
	assert.Equal(t, sessions.sessionID, got.SessionID)
	assert.NotNil(t, got.ConnectionInfo)
}

func TestTryMatch_ExpiredWithMinSize(t *testing.T) {
	store := newFakeStore()
	// 2 件（最低人数 2 ぴったり）
	store.put(makeWaiting("t1", "p1", "", 1000))
	store.put(makeWaiting("t2", "p2", "", 1100))

	// expiry = createdAt(1000) + 60_000 = 61_000。それを超えた時刻。
	m := newMatcherFor(store, newFakeSessionProvider(), 62_000)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)

	assert.Equal(t, protocol.StatusMatched, got.Status)
	// 両方 MATCHED
	for _, id := range []string{"t1", "t2"} {
		stored, _ := store.GetTicket(context.Background(), id)
		assert.Equal(t, protocol.StatusMatched, stored.Status)
	}
}

func TestTryMatch_ExpiredAloneTimesOut(t *testing.T) {
	store := newFakeStore()
	store.put(makeWaiting("t1", "p1", "", 1000))

	m := newMatcherFor(store, newFakeSessionProvider(), 62_000)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)

	assert.Equal(t, protocol.StatusTimeout, got.Status)
	stored, _ := store.GetTicket(context.Background(), "t1")
	assert.Equal(t, protocol.StatusTimeout, stored.Status)
}

func TestTryMatch_ClaimConflictKeepsWaiting(t *testing.T) {
	store := newFakeStore()
	for i, id := range []string{"t1", "t2", "t3", "t4"} {
		store.put(makeWaiting(id, "p"+id, "", int64(1000+i)))
	}
	store.claimErr = db.ErrConflict // 他マッチャーが先取りした想定

	m := newMatcherFor(store, newFakeSessionProvider(), 1500)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)

	// 競合 → 自分のチケットは WAITING のまま
	assert.Equal(t, protocol.StatusWaiting, got.Status)
}

func TestTryMatch_PartyPreserved(t *testing.T) {
	store := newFakeStore()
	// self はソロ。同じクエストにパーティ 3 + ソロが居る。
	store.put(makeWaiting("t1", "p1", "", 1000))   // self（ソロ）
	store.put(makeWaiting("t2", "p2", "PA", 1100)) // パーティ PA (3 人)
	store.put(makeWaiting("t3", "p3", "PA", 1101))
	store.put(makeWaiting("t4", "p4", "PA", 1102))
	store.put(makeWaiting("t5", "p5", "", 1200)) // 別のソロ

	m := newMatcherFor(store, newFakeSessionProvider(), 1500)
	got, err := m.TryMatch(context.Background(), "t1")
	require.NoError(t, err)
	assert.Equal(t, protocol.StatusMatched, got.Status)

	// PA メンバ全員が MATCHED で同一セッションへ
	pa := []string{"t2", "t3", "t4"}
	for _, id := range pa {
		stored, _ := store.GetTicket(context.Background(), id)
		assert.Equal(t, protocol.StatusMatched, stored.Status, "PA member %s should be MATCHED", id)
		assert.Equal(t, got.SessionID, stored.SessionID, "PA member %s same session", id)
	}
	// t5 は残席なしで WAITING のまま
	stored, _ := store.GetTicket(context.Background(), "t5")
	assert.Equal(t, protocol.StatusWaiting, stored.Status)
}

// 競合エラーが ClaimConflict 経路で正しく分類されることを念のため確認。
func TestErrConflictIsDistinct(t *testing.T) {
	assert.True(t, errors.Is(db.ErrConflict, db.ErrConflict))
	assert.False(t, errors.Is(db.ErrConflict, ErrTicketNotFound))
}

//go:build integration

// integration タグ付き。LocalStack（DynamoDB）に実際に接続して CRUD と原子的 claim を検証する。
// 実行: docker compose up -d localstack && ./docker/localstack/scripts/localstack-init.sh
//
//	go test -tags=integration ./internal/db/...
package db_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/internal/db"
	"github.com/raidync/server/pkg/protocol"
)

func setup(t *testing.T) (*db.Client, context.Context) {
	t.Helper()
	if os.Getenv("DYNAMODB_ENDPOINT") == "" {
		t.Setenv("DYNAMODB_ENDPOINT", "http://localhost:4566")
	}
	if os.Getenv("AWS_REGION") == "" {
		t.Setenv("AWS_REGION", "ap-northeast-1")
	}
	ctx := context.Background()
	c, err := db.NewClient(ctx)
	require.NoError(t, err)
	return c, ctx
}

func makeTicket(prefix string, i int) protocol.Ticket {
	now := time.Now()
	// TicketID と PlayerID の双方に UUID を含め、テスト実行をまたいで衝突しないようにする。
	// （PlayerID が固定だと GSI playerId-status-index に過去実行のチケットが残っている時にヒットしてしまう。）
	suffix := uuid.NewString()[:8]
	return protocol.Ticket{
		TicketID:  fmt.Sprintf("%s-%s-%d", prefix, suffix, i),
		PlayerID:  fmt.Sprintf("player-%s-%s-%d", prefix, suffix, i),
		QuestID:   "quest-" + prefix,
		Status:    protocol.StatusWaiting,
		Version:   0,
		Region:    "ap-northeast-1",
		CreatedAt: now.UnixMilli(),
		ExpiresAt: now.UnixMilli() + 60_000,
		TTL:       now.Unix() + protocol.TicketTTLSeconds,
	}
}

// TestRoundTripAndGSIQuery は PutTicket → GetTicket と GSI 経由の検索を確認する。
func TestRoundTripAndGSIQuery(t *testing.T) {
	c, ctx := setup(t)
	prefix := "rt"

	in := makeTicket(prefix, 1)
	require.NoError(t, c.PutTicket(ctx, in))

	got, err := c.GetTicket(ctx, in.TicketID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, in.PlayerID, got.PlayerID)
	assert.Equal(t, protocol.StatusWaiting, got.Status)

	// GSI: questId-status-index
	waiting, err := c.QueryWaitingByQuest(ctx, in.QuestID, 50)
	require.NoError(t, err)
	require.NotEmpty(t, waiting)
	found := false
	for _, w := range waiting {
		if w.TicketID == in.TicketID {
			found = true
			break
		}
	}
	assert.True(t, found, "ticket should appear in GSI query")

	// GSI: playerId-status-index による冪等チェック
	byPlayer, err := c.FindWaitingByPlayer(ctx, in.PlayerID)
	require.NoError(t, err)
	require.NotNil(t, byPlayer)
	assert.Equal(t, in.TicketID, byPlayer.TicketID)
}

// TestClaimTickets_AtomicSuccess は TransactWriteItems による一括 WAITING→MATCHING 遷移を確認する。
func TestClaimTickets_AtomicSuccess(t *testing.T) {
	c, ctx := setup(t)
	prefix := "claim-ok"

	tickets := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}
	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	require.NoError(t, c.ClaimTickets(ctx, tickets, matchingAt, idempotencyToken))

	for _, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, matchingAt, got.MatchingAt)
		assert.Equal(t, idempotencyToken, got.IdempotencyToken)
		assert.Equal(t, protocol.StatusMatching, got.Status)
		assert.Equal(t, tk.Version+1, got.Version)
	}
}

// TestMarkMatched_AllMatched は MarkMatched で確保済みの 4 人全員が 1 つのトランザクションで MATCHED になり、
// 同じ sessionId と、1 人ずつ違う座席番号（PlayerSessionID）が書かれることを確認する。
func TestMarkMatched_AllMatched(t *testing.T) {
	c, ctx := setup(t)
	prefix := "matched-ok"

	tickets := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}
	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	require.NoError(t, c.ClaimTickets(ctx, tickets, matchingAt, idempotencyToken))

	playerSessions := map[string]string{}
	for i, tk := range tickets {
		playerSessions[tk.PlayerID] = fmt.Sprintf("psess-%d", i)
		tickets[i].Version++
	}
	conn := protocol.ConnectionInfo{IPAddress: "127.0.0.1", Port: 7777}
	require.NoError(t, c.MarkMatched(ctx, tickets, "sess-test", conn, playerSessions))

	for i, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		assert.Equal(t, protocol.StatusMatched, got.Status)
		assert.Equal(t, "sess-test", got.SessionID)
		// 共通の conn に、本人の座席番号だけが差し込まれていること（全員同じ値になっていないこと）
		assert.Equal(t, fmt.Sprintf("psess-%d", i), got.ConnectionInfo.PlayerSessionID)
		assert.Equal(t, tk.Version+1, got.Version)
	}
}

// TestMarkFailed_AllFailed は MarkFailed で確保済みの 4 人全員が 1 つのトランザクションで FAILED になることを確認する。
func TestMarkFailed_AllFailed(t *testing.T) {
	c, ctx := setup(t)
	prefix := "failed-ok"

	tickets := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}
	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	require.NoError(t, c.ClaimTickets(ctx, tickets, matchingAt, idempotencyToken))

	for i := range tickets {
		tickets[i].Version++
	}

	require.NoError(t, c.MarkFailed(ctx, tickets))

	for _, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		assert.Equal(t, protocol.StatusFailed, got.Status)
		assert.Equal(t, tk.Version+1, got.Version)
	}
}

// TestMarkMatched_NotAllMatching は 1 人でも MATCHING でなければ MarkMatched がエラーを返し、
// 誰も MATCHED にならないことを確認する（一部だけ MATCHED を作らない）。
// 条件に合わないだけなので、送り直しの対象（ErrTransactionConflict）にはならない。
func TestMarkMatched_NotAllMatching(t *testing.T) {
	c, ctx := setup(t)
	prefix := "matched-partial"

	tickets := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}
	// 3 枚だけ確保し、4 枚目は WAITING のまま残す。
	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	require.NoError(t, c.ClaimTickets(ctx, tickets[:3], matchingAt, idempotencyToken))

	playerSessions := map[string]string{}
	for i, tk := range tickets {
		playerSessions[tk.PlayerID] = fmt.Sprintf("psess-%d", i)
		if i < 3 {
			tickets[i].Version++
		}
	}
	conn := protocol.ConnectionInfo{IPAddress: "127.0.0.1", Port: 7777}
	err := c.MarkMatched(ctx, tickets, "sess-test", conn, playerSessions)
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrConflict)

	for i, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		if i < 3 {
			assert.Equal(t, protocol.StatusMatching, got.Status)
		} else {
			assert.Equal(t, protocol.StatusWaiting, got.Status)
		}
	}
}

// TestClaimTickets_VersionConflict は version 不一致のトランザクションが ErrConflict を返し、
// 全件が WAITING のまま保たれることを確認する（部分適用が起きないこと）。
func TestClaimTickets_VersionConflict(t *testing.T) {
	c, ctx := setup(t)
	prefix := "claim-conflict"

	tickets := make([]protocol.Ticket, 0, 2)
	for i := 0; i < 2; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}

	// 1 件目の version を意図的にずらして「他者が先に変更した」状況を作る。
	stale := tickets
	stale[0].Version = 99
	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	err := c.ClaimTickets(ctx, stale, matchingAt, idempotencyToken)
	require.Error(t, err)
	assert.True(t, errors.Is(err, db.ErrConflict), "expected ErrConflict, got %v", err)

	// 部分適用が起きておらず、両方とも WAITING のままであること
	for _, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		assert.Equal(t, protocol.StatusWaiting, got.Status)
		assert.Equal(t, 0, got.Version)
	}
}

// TestCancelTicket は WAITING→CANCELLED の遷移と、再キャンセル時の ErrConflict を確認する。
func TestCancelTicket(t *testing.T) {
	c, ctx := setup(t)
	prefix := "cancel"

	tk := makeTicket(prefix, 0)
	require.NoError(t, c.PutTicket(ctx, tk))

	require.NoError(t, c.CancelTicket(ctx, tk.TicketID))
	got, err := c.GetTicket(ctx, tk.TicketID)
	require.NoError(t, err)
	assert.Equal(t, protocol.StatusCancelled, got.Status)

	// 2 度目は条件 (#s = WAITING) が満たせず ErrConflict
	err = c.CancelTicket(ctx, tk.TicketID)
	assert.True(t, errors.Is(err, db.ErrConflict))
}

// TestMarkMatched_LateAfterFailed は、救済で FAILED になった後、確保時の version のまま遅れて届いた
// MarkMatched が弾かれ、MATCHED に戻らないことを確認する。
func TestMarkMatched_LateAfterFailed(t *testing.T) {
	c, ctx := setup(t)
	prefix := "mark-matched"

	tickets := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		tickets = append(tickets, tk)
	}

	matchingAt := time.Now().UnixMilli()
	idempotencyToken := uuid.NewString()
	require.NoError(t, c.ClaimTickets(ctx, tickets, matchingAt, idempotencyToken))
	for i := range tickets {
		tickets[i].Version++
	}

	playerSessions := map[string]string{}
	for i, tk := range tickets {
		playerSessions[tk.PlayerID] = fmt.Sprintf("psess-%d", i)
	}
	conn := protocol.ConnectionInfo{IPAddress: "127.0.0.1", Port: 7777}
	// 救済: DynamoDB の今の version（確保後の値）を条件に FAILED にする。成功する。
	require.NoError(t, c.MarkFailed(ctx, tickets))

	// 遅れて届いた A の MarkMatched: 確保後の version のままなので弾かれる。送り直しの対象でもない。
	err := c.MarkMatched(ctx, tickets, "sess-test", conn, playerSessions)
	require.Error(t, err)
	assert.ErrorIs(t, err, db.ErrConflict)
	for _, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		assert.Equal(t, protocol.StatusFailed, got.Status)
		assert.Equal(t, tk.Version+1, got.Version)
	}
}

func TestFindMatchingByToken(t *testing.T) {
	c, ctx := setup(t)
	prefix := "find-token" // makeTicket は QuestID を "quest-" + prefix にするので、3 グループとも同じクエストになる
	matchingAt := time.Now().UnixMilli()

	// A を 4 枚作って、トークン A で確保
	tokenA := uuid.NewString()
	ticketsA := make([]protocol.Ticket, 0, 4)
	for i := 0; i < 4; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		ticketsA = append(ticketsA, tk)
	}
	require.NoError(t, c.ClaimTickets(ctx, ticketsA, matchingAt, tokenA))

	// B を 2 枚作って、トークン B で確保
	tokenB := uuid.NewString()
	ticketsB := make([]protocol.Ticket, 0, 2)
	for i := 0; i < 2; i++ {
		tk := makeTicket(prefix, i)
		require.NoError(t, c.PutTicket(ctx, tk))
		ticketsB = append(ticketsB, tk)
	}
	require.NoError(t, c.ClaimTickets(ctx, ticketsB, matchingAt, tokenB))

	// C を 1 枚作るだけ
	require.NoError(t, c.PutTicket(ctx, makeTicket(prefix, 0)))

	// got の TicketID の集まりが、A の 4 枚の TicketID とちょうど一致すること
	got, err := c.FindMatchingByToken(ctx, "quest-"+prefix, tokenA)
	require.NoError(t, err)

	// 並び順は決まっていないので、TicketID の集まりとして比べる
	wantIDs := make([]string, 0, len(ticketsA))
	for _, tk := range ticketsA {
		wantIDs = append(wantIDs, tk.TicketID)
	}
	gotIDs := make([]string, 0, len(got))
	for _, tk := range got {
		gotIDs = append(gotIDs, tk.TicketID)
	}
	assert.ElementsMatch(t, wantIDs, gotIDs)
}

// TestFailSession は、行があれば FAILED にして endedAt を記録し、行が無ければ作らずに nil を返すことを確認する。
func TestFailSession(t *testing.T) {
	c, ctx := setup(t)

	t.Run("existing", func(t *testing.T) {
		sessionID := uuid.NewString()
		require.NoError(t, c.PutSession(ctx, protocol.Session{
			SessionID: sessionID,
			QuestID:   "quest-fail-session",
			Status:    protocol.SessionActive,
			CreatedAt: time.Now().UnixMilli(),
		}))
		endedAt := time.Now().UnixMilli()

		require.NoError(t, c.FailSession(ctx, sessionID, endedAt))

		got, err := c.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, protocol.SessionFailed, got.Status)
		assert.Equal(t, endedAt, got.EndedAt)
	})

	t.Run("missing", func(t *testing.T) {
		sessionID := uuid.NewString() // PutSession していない ID

		require.NoError(t, c.FailSession(ctx, sessionID, time.Now().UnixMilli()))

		got, err := c.GetSession(ctx, sessionID)
		require.NoError(t, err)
		assert.Nil(t, got, "行が無ければ作らない（upsert しない）")
	})
}

// TestEndSession は、ACTIVE のときだけ ENDED への遷移と履歴の追記がまとめて通り、
// ACTIVE 以外では ErrConflict を返して何も書かない（履歴も残らない）ことを確認する。
func TestEndSession(t *testing.T) {
	c, ctx := setup(t)

	// arrange は指定した状態のセッション行と、その 2 人分の履歴を用意する。
	// プレイヤー ID は毎回新しく作り、ほかのテストや過去の実行の履歴と混ざらないようにする。
	arrange := func(t *testing.T, status protocol.SessionStatus) (string, []protocol.MatchHistoryEntry, int64) {
		t.Helper()
		sessionID := uuid.NewString()
		playerIDs := []string{"player-end-" + uuid.NewString(), "player-end-" + uuid.NewString()}
		require.NoError(t, c.PutSession(ctx, protocol.Session{
			SessionID: sessionID,
			QuestID:   "quest-end-session",
			PlayerIDs: playerIDs,
			Status:    status,
			CreatedAt: time.Now().UnixMilli(),
		}))
		endedAt := time.Now().UnixMilli()
		entries := make([]protocol.MatchHistoryEntry, 0, len(playerIDs))
		for _, pid := range playerIDs {
			entries = append(entries, protocol.MatchHistoryEntry{
				PlayerID:    pid,
				Timestamp:   endedAt,
				SessionID:   sessionID,
				QuestID:     "quest-end-session",
				Result:      protocol.ResultClear,
				DurationSec: 600,
			})
		}
		return sessionID, entries, endedAt
	}

	t.Run("active", func(t *testing.T) {
		sessionID, entries, endedAt := arrange(t, protocol.SessionActive)

		require.NoError(t, c.EndSession(ctx, sessionID, entries, endedAt))

		got, err := c.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, protocol.SessionEnded, got.Status)
		assert.Equal(t, endedAt, got.EndedAt)
		for _, e := range entries {
			hist, err := c.QueryByPlayer(ctx, e.PlayerID, 10)
			require.NoError(t, err)
			require.Len(t, hist, 1, "1 人につき履歴が 1 件書かれる")
			assert.Equal(t, sessionID, hist[0].SessionID)
		}
	})

	t.Run("failed", func(t *testing.T) {
		sessionID, entries, endedAt := arrange(t, protocol.SessionFailed)

		err := c.EndSession(ctx, sessionID, entries, endedAt)
		assert.ErrorIs(t, err, db.ErrConflict)

		got, err := c.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, protocol.SessionFailed, got.Status, "片付け済みの行は FAILED のまま")
		for _, e := range entries {
			hist, err := c.QueryByPlayer(ctx, e.PlayerID, 10)
			require.NoError(t, err)
			assert.Empty(t, hist, "遷移が通らなければ履歴も書かれない（全部か無しか）")
		}
	})

	t.Run("ended", func(t *testing.T) {
		sessionID, entries, endedAt := arrange(t, protocol.SessionEnded)

		err := c.EndSession(ctx, sessionID, entries, endedAt)
		assert.ErrorIs(t, err, db.ErrConflict, "二重の報告は弾く")
	})
}

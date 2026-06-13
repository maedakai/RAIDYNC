//go:build integration

// integration タグ付き。LocalStack（DynamoDB）に実際に接続して CRUD と原子的 claim を検証する。
// 実行: docker compose up -d localstack && ./docker/localstack/scripts/localstack-init.sh
//      go test -tags=integration ./internal/db/...
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

	require.NoError(t, c.ClaimTickets(ctx, tickets))

	for _, tk := range tickets {
		got, err := c.GetTicket(ctx, tk.TicketID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, protocol.StatusMatching, got.Status)
		assert.Equal(t, tk.Version+1, got.Version)
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

	err := c.ClaimTickets(ctx, stale)
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

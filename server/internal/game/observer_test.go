package game

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/pkg/protocol"
)

// recordingObserver は参加/離脱を記録するテスト用 PlayerObserver。
// probe はコールバック内で呼ばれ、Session のロックが解放済み（＝ホットパス外）で
// あることを検証するのに使う（ロック保持中なら PlayerCount() でデッドロックする）。
type recordingObserver struct {
	mu     sync.Mutex
	joins  []playerRef
	leaves []playerRef
	probe  func()
}

func (o *recordingObserver) OnPlayerJoin(_, playerID, playerSessionID string) {
	if o.probe != nil {
		o.probe()
	}
	o.mu.Lock()
	o.joins = append(o.joins, playerRef{playerID, playerSessionID})
	o.mu.Unlock()
}

func (o *recordingObserver) OnPlayerLeave(_, playerID, playerSessionID string) {
	if o.probe != nil {
		o.probe()
	}
	o.mu.Lock()
	o.leaves = append(o.leaves, playerRef{playerID, playerSessionID})
	o.mu.Unlock()
}

func TestObserver_JoinLeaveWithPlayerSessions(t *testing.T) {
	obs := &recordingObserver{}
	var s *Session
	// ロックが解放されていなければ PlayerCount() が s.mu を取れずデッドロックする＝off-lock 保証。
	obs.probe = func() { _ = s.PlayerCount() }

	s = NewSession("sess-1", "quest-001", []string{"p1", "p2"}, 0,
		WithPlayerObserver(obs),
		WithPlayerSessions(map[string]string{"p1": "psess-1", "p2": "psess-2"}),
	)

	// p1 初回パケット → join（psid が対応表から解決される）
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 0))
	// p1 2 回目 → 追加の join は起きない
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 2, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 100))
	// p2 → join
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p2", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5002), 9000))

	assert.Equal(t, []playerRef{{"p1", "psess-1"}, {"p2", "psess-2"}}, obs.joins)
	assert.Empty(t, obs.leaves)

	// p1 がアイドルタイムアウトで離脱 → OnPlayerLeave
	s.PruneIdle(11_000)
	assert.Equal(t, []playerRef{{"p1", "psess-1"}}, obs.leaves)

	// セッション終了 → 残る p2 も離脱扱いで枠を返す
	_, _, ok := s.EndMessage(protocol.EndTimeout, maxSessionMs)
	require.True(t, ok)
	assert.Equal(t, []playerRef{{"p1", "psess-1"}, {"p2", "psess-2"}}, obs.leaves)
}

func TestObserver_NoFireForNonRoster(t *testing.T) {
	obs := &recordingObserver{}
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0, WithPlayerObserver(obs))

	_ = s.Ingest(moveMsg("sess-1", "intruder", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5009), 10)
	assert.Empty(t, obs.joins, "roster 外は join を発火しない")
}

func TestObserver_EmptyPsidWhenUnmapped(t *testing.T) {
	obs := &recordingObserver{}
	// WithPlayerSessions 未指定（ローカル相当）→ psid は空。Bridge 側が SDK 呼び出しをスキップする。
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0, WithPlayerObserver(obs))

	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 0))
	require.Len(t, obs.joins, 1)
	assert.Equal(t, "", obs.joins[0].playerSessionID)
}

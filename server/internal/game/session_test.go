package game

import (
	"encoding/json"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/pkg/protocol"
)

func addr(port int) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
}

func moveMsg(sid, pid string, seq uint32, pos protocol.Vector3, state protocol.PlayerActorState) *protocol.GameMessage {
	payload, _ := json.Marshal(protocol.PlayerMovePayload{Position: pos, State: state})
	return &protocol.GameMessage{
		Type:      protocol.MsgPlayerMove,
		SessionID: sid,
		PlayerID:  pid,
		Seq:       seq,
		Payload:   payload,
	}
}

func actionMsg(sid, pid, target string, seq uint32, dmg int) *protocol.GameMessage {
	payload, _ := json.Marshal(protocol.PlayerActionPayload{Action: "attack", TargetID: target, Damage: dmg})
	return &protocol.GameMessage{
		Type:      protocol.MsgPlayerAction,
		SessionID: sid,
		PlayerID:  pid,
		Seq:       seq,
		Payload:   payload,
	}
}

func TestIngest_MoveUpdatesState(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1", "p2"}, 0)

	pos := protocol.Vector3{X: 1, Y: 2, Z: 3}
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, pos, protocol.ActorMoving), addr(5001), 10))

	msg, recipients, ok := s.Snapshot(20)
	require.True(t, ok)
	assert.Len(t, recipients, 1)

	var pl protocol.SessionStatePayload
	require.NoError(t, json.Unmarshal(msg.Payload, &pl))
	require.Len(t, pl.Players, 1)
	assert.Equal(t, pos, pl.Players[0].Position)
	assert.Equal(t, protocol.ActorMoving, pl.Players[0].State)
	assert.Equal(t, 100, pl.Players[0].Health)
}

func TestIngest_RejectsNonRoster(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	err := s.Ingest(moveMsg("sess-1", "intruder", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5009), 10)
	assert.ErrorIs(t, err, ErrNotInRoster)
	assert.Equal(t, 0, s.PlayerCount())
}

func TestIngest_DropsStaleSeq(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)

	newPos := protocol.Vector3{X: 5}
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 5, newPos, protocol.ActorMoving), addr(5001), 10))
	// seq=3 は受理済み最大(5)より小さい → 破棄される（エラーにはならない）
	oldPos := protocol.Vector3{X: 1}
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 3, oldPos, protocol.ActorIdle), addr(5001), 11))

	_, _, ok := s.Snapshot(20)
	require.True(t, ok)
	msg, _, _ := s.Snapshot(20)
	var pl protocol.SessionStatePayload
	require.NoError(t, json.Unmarshal(msg.Payload, &pl))
	assert.Equal(t, newPos, pl.Players[0].Position, "古いパケットで上書きされてはならない")
}

func TestIngest_ActionAppliesDamageAndDeath(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1", "p2"}, 0)
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p2", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5002), 10))

	require.NoError(t, s.Ingest(actionMsg("sess-1", "p1", "p2", 1, 30), addr(5001), 11))
	assert.Equal(t, 70, healthOf(t, s, "p2"))

	// 致死ダメージで dead へ
	require.NoError(t, s.Ingest(actionMsg("sess-1", "p1", "p2", 2, 100), addr(5001), 12))
	st := stateOf(t, s, "p2")
	assert.Equal(t, 0, st.Health)
	assert.Equal(t, protocol.ActorDead, st.State)
}

func TestSnapshot_IncrementsSeq(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 10))

	m1, _, _ := s.Snapshot(20)
	m2, _, _ := s.Snapshot(30)
	assert.Equal(t, uint32(1), m1.Seq)
	assert.Equal(t, uint32(2), m2.Seq)
	assert.Equal(t, protocol.MsgSessionState, m1.Type)
}

func TestSnapshot_EmptyWhenNoPlayers(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	_, _, ok := s.Snapshot(20)
	assert.False(t, ok)
}

func TestPruneIdle_RemovesTimedOut(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1", "p2"}, 0)
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 0))
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p2", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5002), 9000))

	// now=11000: p1 は 11s 無音 → 除去、p2 は 2s → 残存
	removed := s.PruneIdle(11_000)
	assert.Equal(t, 1, removed)
	assert.Equal(t, 1, s.PlayerCount())
}

func TestEndReason_TimeoutAfterMax(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	_, ok := s.EndReason(maxSessionMs - 1)
	assert.False(t, ok)

	reason, ok := s.EndReason(maxSessionMs)
	require.True(t, ok)
	assert.Equal(t, protocol.EndTimeout, reason)
}

func TestEndMessage_Idempotent(t *testing.T) {
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	require.NoError(t, s.Ingest(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 0))

	msg, recipients, ok := s.EndMessage(protocol.EndTimeout, maxSessionMs)
	require.True(t, ok)
	assert.Equal(t, protocol.MsgSessionEnd, msg.Type)
	assert.Len(t, recipients, 1)
	assert.True(t, s.Ended())

	_, _, ok2 := s.EndMessage(protocol.EndTimeout, maxSessionMs)
	assert.False(t, ok2, "2 回目は冪等に false")

	// 終了後の取り込みは拒否
	err := s.Ingest(moveMsg("sess-1", "p1", 2, protocol.Vector3{}, protocol.ActorIdle), addr(5001), maxSessionMs+1)
	assert.ErrorIs(t, err, ErrSessionEnded)
}

// --- helpers ---

func snapshotStates(t *testing.T, s *Session) []protocol.PlayerState {
	t.Helper()
	msg, _, ok := s.Snapshot(0)
	require.True(t, ok)
	var pl protocol.SessionStatePayload
	require.NoError(t, json.Unmarshal(msg.Payload, &pl))
	return pl.Players
}

func stateOf(t *testing.T, s *Session, pid string) protocol.PlayerState {
	t.Helper()
	for _, p := range snapshotStates(t, s) {
		if p.PlayerID == pid {
			return p
		}
	}
	t.Fatalf("player %s not found", pid)
	return protocol.PlayerState{}
}

func healthOf(t *testing.T, s *Session, pid string) int {
	t.Helper()
	return stateOf(t, s, pid).Health
}

package game

import (
	"encoding/json"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/pkg/protocol"
)

// fakeSender は送信内容を記録するテスト用 Sender。
type fakeSender struct {
	mu   sync.Mutex
	sent []sentPacket
}

type sentPacket struct {
	addr *net.UDPAddr
	msg  protocol.GameMessage
}

func (f *fakeSender) SendTo(a *net.UDPAddr, data []byte) error {
	var m protocol.GameMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sentPacket{addr: a, msg: m})
	return nil
}

func (f *fakeSender) byType(t protocol.GameMessageType) []sentPacket {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []sentPacket
	for _, p := range f.sent {
		if p.msg.Type == t {
			out = append(out, p)
		}
	}
	return out
}

func TestHub_RouteUnknownSession(t *testing.T) {
	hub := NewHub(&fakeSender{})
	err := hub.Route(moveMsg("ghost", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), 0)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestHub_TickBroadcastsToAllPlayers(t *testing.T) {
	fs := &fakeSender{}
	hub := NewHub(fs)
	s := NewSession("sess-1", "quest-001", []string{"p1", "p2"}, 0)
	hub.Register(s)

	require.NoError(t, hub.Route(moveMsg("sess-1", "p1", 1, protocol.Vector3{X: 1}, protocol.ActorMoving), addr(5001), 10))
	require.NoError(t, hub.Route(moveMsg("sess-1", "p2", 1, protocol.Vector3{X: 2}, protocol.ActorMoving), addr(5002), 10))

	hub.Tick(50)

	states := fs.byType(protocol.MsgSessionState)
	require.Len(t, states, 2, "2 プレイヤー全員へ SESSION_STATE が届く")
	// ペイロードには両プレイヤーが含まれる
	var pl protocol.SessionStatePayload
	require.NoError(t, json.Unmarshal(states[0].msg.Payload, &pl))
	assert.Len(t, pl.Players, 2)
}

func TestHub_TickEndsSessionAtTimeout(t *testing.T) {
	fs := &fakeSender{}
	hub := NewHub(fs)
	s := NewSession("sess-1", "quest-001", []string{"p1"}, 0)
	hub.Register(s)
	// タイムアウト時点でもプレイヤーは接続中（直前まで送信している）想定。
	require.NoError(t, hub.Route(moveMsg("sess-1", "p1", 1, protocol.Vector3{}, protocol.ActorIdle), addr(5001), maxSessionMs))

	hub.Tick(maxSessionMs) // 20 分到達

	ends := fs.byType(protocol.MsgSessionEnd)
	require.Len(t, ends, 1)
	var pl protocol.SessionEndPayload
	require.NoError(t, json.Unmarshal(ends[0].msg.Payload, &pl))
	assert.Equal(t, protocol.EndTimeout, pl.Reason)
	assert.Equal(t, 0, hub.SessionCount(), "終了セッションは登録解除される")
}

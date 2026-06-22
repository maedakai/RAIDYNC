package gameserver_test

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/raidync/server/internal/gameserver"
	"github.com/raidync/server/pkg/protocol"
)

// TestLoopback_MoveThenSessionState は実ループバック UDP で
// 「クライアントが PLAYER_MOVE を送る → サーバーが SESSION_STATE を返す」往復を検証する。
func TestLoopback_MoveThenSessionState(t *testing.T) {
	srv, err := gameserver.New(gameserver.Config{
		BindAddr:  "127.0.0.1:0", // ephemeral ポート（ファイアウォール回避のため loopback 限定）
		SessionID: "sess-loop",
		QuestID:   "quest-001",
		Roster:    []string{"p1", "p2"},
	})
	require.NoError(t, err)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Serve(ctx)

	// クライアント役の UDP ソケットを 2 つ用意して接続。
	c1 := dial(t, srv.LocalAddr())
	defer c1.Close()
	c2 := dial(t, srv.LocalAddr())
	defer c2.Close()

	send(t, c1, moveMsg("sess-loop", "p1", 1, protocol.Vector3{X: 1, Y: 0, Z: 0}))
	send(t, c2, moveMsg("sess-loop", "p2", 1, protocol.Vector3{X: 0, Y: 0, Z: 9}))

	// 50ms 周期のブロードキャストを待って受信。複数受け取り得るので最新（最大 seq）を採用。
	state := recvSessionState(t, c1, 2*time.Second)
	require.NotNil(t, state)

	var pl protocol.SessionStatePayload
	require.NoError(t, json.Unmarshal(state.Payload, &pl))
	assert.Len(t, pl.Players, 2, "両プレイヤーがスナップショットに含まれる")

	byID := map[string]protocol.PlayerState{}
	for _, p := range pl.Players {
		byID[p.PlayerID] = p
	}
	assert.Equal(t, float64(1), byID["p1"].Position.X)
	assert.Equal(t, float64(9), byID["p2"].Position.Z)
	assert.Equal(t, 100, byID["p1"].Health)
}

func dial(t *testing.T, server *net.UDPAddr) *net.UDPConn {
	t.Helper()
	c, err := net.DialUDP("udp", nil, server)
	require.NoError(t, err)
	return c
}

func send(t *testing.T, c *net.UDPConn, msg *protocol.GameMessage) {
	t.Helper()
	data, err := json.Marshal(msg)
	require.NoError(t, err)
	_, err = c.Write(data)
	require.NoError(t, err)
}

// recvSessionState は SESSION_STATE を 1 つ受信して返す（締切まで届かなければ fail）。
func recvSessionState(t *testing.T, c *net.UDPConn, timeout time.Duration) *protocol.GameMessage {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(timeout)))
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		require.NoError(t, err, "SESSION_STATE を時間内に受信できなかった")
		var msg protocol.GameMessage
		require.NoError(t, json.Unmarshal(buf[:n], &msg))
		if msg.Type == protocol.MsgSessionState {
			return &msg
		}
	}
}

func moveMsg(sid, pid string, seq uint32, pos protocol.Vector3) *protocol.GameMessage {
	payload, _ := json.Marshal(protocol.PlayerMovePayload{Position: pos, State: protocol.ActorMoving})
	return &protocol.GameMessage{
		Type:      protocol.MsgPlayerMove,
		SessionID: sid,
		PlayerID:  pid,
		Seq:       seq,
		Payload:   payload,
	}
}

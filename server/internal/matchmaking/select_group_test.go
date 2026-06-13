package matchmaking

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/raidync/server/pkg/protocol"
)

// tk はテスト記述を短くするためのチケット組み立てヘルパー。
func tk(id, player, party string, createdAt int64) protocol.Ticket {
	return protocol.Ticket{
		TicketID:  id,
		PlayerID:  player,
		PartyID:   party,
		QuestID:   "q1",
		Status:    protocol.StatusWaiting,
		CreatedAt: createdAt,
	}
}

func ids(group []protocol.Ticket) []string {
	out := make([]string, 0, len(group))
	for _, g := range group {
		out = append(out, g.TicketID)
	}
	return out
}

func TestSelectGroup(t *testing.T) {
	type tc struct {
		name       string
		self       protocol.Ticket
		candidates []protocol.Ticket
		wantIDs    []string // 先頭は必ず self
	}
	cases := []tc{
		{
			name:       "self only — 候補なし",
			self:       tk("t1", "p1", "", 100),
			candidates: nil,
			wantIDs:    []string{"t1"},
		},
		{
			name: "solo 4 人 FIFO 詰め",
			self: tk("t1", "p1", "", 100),
			candidates: []protocol.Ticket{
				tk("t2", "p2", "", 200),
				tk("t3", "p3", "", 150),
				tk("t4", "p4", "", 300),
				tk("t5", "p5", "", 400),
			},
			// createdAt 昇順で t3(150), t2(200), t4(300) が入り 4 人で打ち止め
			wantIDs: []string{"t1", "t3", "t2", "t4"},
		},
		{
			name: "self のパーティ仲間を最優先",
			self: tk("t1", "p1", "PA", 500), // self は新参だが…
			candidates: []protocol.Ticket{
				tk("t2", "p2", "PA", 100), // 同パーティ（古い）
				tk("t3", "p3", "PA", 200), // 同パーティ
				tk("t4", "p4", "", 50),    // ソロ（最古）
			},
			// 同パーティ仲間を先に詰め、残り 1 席にソロが入る
			wantIDs: []string{"t1", "t2", "t3", "t4"},
		},
		{
			name: "他人のパーティは丸ごと入る時だけ追加",
			self: tk("t1", "p1", "", 100),
			candidates: []protocol.Ticket{
				tk("t2", "p2", "PB", 200), // パーティ PB (3 人)
				tk("t3", "p3", "PB", 210),
				tk("t4", "p4", "PB", 220),
				tk("t5", "p5", "", 300), // ソロ
			},
			// 残席 3。PB は 3 人で丸ごと入る → self + PB(3) = 4 人
			wantIDs: []string{"t1", "t2", "t3", "t4"},
		},
		{
			name: "残席に収まらない他人パーティは分割せずスキップ",
			self: tk("t1", "p1", "", 100),
			candidates: []protocol.Ticket{
				tk("t2", "p2", "", 150), // ソロ
				tk("t3", "p3", "PC", 200), // パーティ PC (3 人) — 残席 2 に入らない
				tk("t4", "p4", "PC", 210),
				tk("t5", "p5", "PC", 220),
				tk("t6", "p6", "", 250), // ソロ
			},
			// self + t2(solo) = 2 人。PC は 3 人で残席 2 に入らずスキップ。t6 が入って 3 人。
			wantIDs: []string{"t1", "t2", "t6"},
		},
		{
			name: "self のパーティが既に 4 人なら他は入らない",
			self: tk("t1", "p1", "PA", 100),
			candidates: []protocol.Ticket{
				tk("t2", "p2", "PA", 110),
				tk("t3", "p3", "PA", 120),
				tk("t4", "p4", "PA", 130),
				tk("t5", "p5", "", 200), // 入る余地なし
			},
			wantIDs: []string{"t1", "t2", "t3", "t4"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := selectGroup(&c.self, c.candidates)
			assert.Equal(t, c.wantIDs, ids(got))
			// self は常に先頭
			if len(got) > 0 {
				assert.Equal(t, c.self.TicketID, got[0].TicketID)
			}
		})
	}
}

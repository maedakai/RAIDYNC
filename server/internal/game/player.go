// Package game はゲームセッションの実行時状態とリアルタイム同期のコアロジックを持つ。
// UDP の送受信そのものはサーバー層（cmd/gameserver）に閉じ込め、本パッケージは
// 「受信メッセージで状態を更新し、ブロードキャスト用スナップショットを作る」ことに集中する。
package game

import (
	"net"

	"github.com/raidync/server/pkg/protocol"
)

// player はセッション内の 1 プレイヤーの実行時状態。
// 全フィールドは所属する Session の Mutex 配下でのみ読み書きする。
type player struct {
	id       string
	addr     *net.UDPAddr // 直近に観測した送信元。UDP は接続レスなのでパケットごとに更新する
	position protocol.Vector3
	rotation protocol.Vector3
	health   int
	state    protocol.PlayerActorState
	seen     bool   // 1 通でもパケットを受理したか（seq ゲートの初回判定用）
	lastSeq  uint32 // 受理済みの最大シーケンス番号（これ以下は古い/重複として破棄）
	lastSeen int64  // 直近パケット受信時刻（UnixTime ms）。離脱検出に使う
}

func newPlayer(id string, now int64) *player {
	return &player{
		id:       id,
		health:   100,
		state:    protocol.ActorIdle,
		lastSeen: now,
	}
}

// snapshot は SESSION_STATE 用の不変ビューを返す。
func (p *player) snapshot() protocol.PlayerState {
	return protocol.PlayerState{
		PlayerID: p.id,
		Position: p.position,
		Rotation: p.rotation,
		Health:   p.health,
		State:    p.state,
	}
}

// applyDamage は health を減算し、0 以下なら dead に遷移する。
func (p *player) applyDamage(dmg int) {
	if dmg <= 0 || p.state == protocol.ActorDead {
		return
	}
	p.health -= dmg
	if p.health <= 0 {
		p.health = 0
		p.state = protocol.ActorDead
	}
}

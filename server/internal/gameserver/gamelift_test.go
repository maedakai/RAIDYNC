package gameserver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/model"

	"github.com/raidync/server/pkg/protocol"
)

func TestParseSessionConfig(t *testing.T) {
	gs := model.GameSession{
		GameSessionID: "gs-1",
		GameProperties: map[string]string{
			"questId":        "quest-003",
			"playerIds":      "p1, p2 ,p3",               // 余分な空白も許容
			"playerSessions": "p1=psess-1, p2 = psess-2", // playerId→psid（空白許容）
		},
	}
	questID, roster, psids := parseSessionConfig(gs)
	assert.Equal(t, "quest-003", questID)
	assert.Equal(t, []string{"p1", "p2", "p3"}, roster)
	assert.Equal(t, map[string]string{"p1": "psess-1", "p2": "psess-2"}, psids)
}

func TestParseSessionConfig_Defaults(t *testing.T) {
	gs := model.GameSession{GameSessionID: "gs-2"} // GameProperties は nil
	questID, roster, psids := parseSessionConfig(gs)
	assert.Equal(t, "unknown", questID)
	assert.Empty(t, roster)
	assert.Empty(t, psids) // playerSessions 未指定なら空マップ
}

func TestGameLiftBridge_OnSessionEndStops(t *testing.T) {
	stopped := 0
	b := NewGameLiftBridge(nil, 0, func() { stopped++ })

	b.OnSessionEnd("sess-1", protocol.EndTimeout, 1200)

	assert.Equal(t, 1, stopped, "セッションが終わったら受信ループを止める（ProcessEnding は main が呼ぶ）")
}

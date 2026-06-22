package gameserver

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk/model"
)

func TestParseSessionConfig(t *testing.T) {
	gs := model.GameSession{
		GameSessionID: "gs-1",
		GameProperties: map[string]string{
			"questId":   "quest-003",
			"playerIds": "p1, p2 ,p3", // 余分な空白も許容
		},
	}
	questID, roster := parseSessionConfig(gs)
	assert.Equal(t, "quest-003", questID)
	assert.Equal(t, []string{"p1", "p2", "p3"}, roster)
}

func TestParseSessionConfig_Defaults(t *testing.T) {
	gs := model.GameSession{GameSessionID: "gs-2"} // GameProperties は nil
	questID, roster := parseSessionConfig(gs)
	assert.Equal(t, "unknown", questID)
	assert.Empty(t, roster)
}

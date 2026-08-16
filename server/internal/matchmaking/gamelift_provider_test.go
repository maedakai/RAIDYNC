package matchmaking

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/gamelift"
	gltypes "github.com/aws/aws-sdk-go-v2/service/gamelift/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeGameLift は gameLiftAPI を満たすフェイク。入力を記録し、事前設定した出力/エラーを返す。
type fakeGameLift struct {
	gsIn  *gamelift.CreateGameSessionInput
	psIn  *gamelift.CreatePlayerSessionsInput
	gsOut *gamelift.CreateGameSessionOutput
	psOut *gamelift.CreatePlayerSessionsOutput
	gsErr error
	psErr error
}

func (f *fakeGameLift) CreateGameSession(_ context.Context, in *gamelift.CreateGameSessionInput, _ ...func(*gamelift.Options)) (*gamelift.CreateGameSessionOutput, error) {
	f.gsIn = in
	return f.gsOut, f.gsErr
}

func (f *fakeGameLift) CreatePlayerSessions(_ context.Context, in *gamelift.CreatePlayerSessionsInput, _ ...func(*gamelift.Options)) (*gamelift.CreatePlayerSessionsOutput, error) {
	f.psIn = in
	return f.psOut, f.psErr
}

func activeGameSession() *gamelift.CreateGameSessionOutput {
	return &gamelift.CreateGameSessionOutput{
		GameSession: &gltypes.GameSession{
			GameSessionId: aws.String("gs-1"),
			IpAddress:     aws.String("10.0.0.9"),
			Port:          aws.Int32(7777),
			Status:        gltypes.GameSessionStatusActive,
		},
	}
}

func TestGameLiftProvider_CreateGameSession(t *testing.T) {
	fake := &fakeGameLift{
		gsOut: activeGameSession(),
		psOut: &gamelift.CreatePlayerSessionsOutput{PlayerSessions: []gltypes.PlayerSession{
			{PlayerId: aws.String("p1"), PlayerSessionId: aws.String("psess-1")},
			{PlayerId: aws.String("p2"), PlayerSessionId: aws.String("psess-2")},
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-123", "custom-raidync-local")

	sessionID, conn, psids, err := p.CreateGameSession(context.Background(), "quest-rathalos", []string{"p1", "p2"})
	require.NoError(t, err)

	// 出力: 共通接続先（psid 空）＋ プレイヤー毎の psid
	assert.Equal(t, "gs-1", sessionID)
	assert.Equal(t, "10.0.0.9", conn.IPAddress)
	assert.Equal(t, 7777, conn.Port)
	assert.Empty(t, conn.PlayerSessionID, "共通 conn に psid は載せない")
	assert.Equal(t, map[string]string{"p1": "psess-1", "p2": "psess-2"}, psids)

	// 入力: fleetId / location / GameProperties(questId, playerIds)
	require.NotNil(t, fake.gsIn)
	assert.Equal(t, "fleet-123", aws.ToString(fake.gsIn.FleetId))
	assert.Equal(t, "custom-raidync-local", aws.ToString(fake.gsIn.Location))
	assert.Equal(t, map[string]string{"questId": "quest-rathalos", "playerIds": "p1,p2"}, gamePropsToMap(fake.gsIn.GameProperties))
	// CreatePlayerSessions に roster がそのまま渡ること
	require.NotNil(t, fake.psIn)
	assert.Equal(t, "gs-1", aws.ToString(fake.psIn.GameSessionId))
	assert.Equal(t, []string{"p1", "p2"}, fake.psIn.PlayerIds)
}

func TestGameLiftProvider_ManagedEC2OmitsLocation(t *testing.T) {
	fake := &fakeGameLift{
		gsOut: activeGameSession(),
		psOut: &gamelift.CreatePlayerSessionsOutput{PlayerSessions: []gltypes.PlayerSession{
			{PlayerId: aws.String("p1"), PlayerSessionId: aws.String("psess-1")},
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-123", "") // location 空 = managed EC2

	_, _, _, err := p.CreateGameSession(context.Background(), "q", []string{"p1"})
	require.NoError(t, err)
	assert.Nil(t, fake.gsIn.Location, "location 空なら Location は省略(nil)")
}

func TestGameLiftProvider_ErrorWhenNotAddressable(t *testing.T) {
	fake := &fakeGameLift{
		gsOut: &gamelift.CreateGameSessionOutput{GameSession: &gltypes.GameSession{
			GameSessionId: aws.String("gs-1"),
			Status:        gltypes.GameSessionStatusActivating, // IP/Port 未確定
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-123", "")

	_, _, _, err := p.CreateGameSession(context.Background(), "q", []string{"p1"})
	require.Error(t, err)
	assert.Nil(t, fake.psIn, "接続先未確定なら CreatePlayerSessions は呼ばない")
}

func TestGameLiftProvider_PropagatesCreateError(t *testing.T) {
	fake := &fakeGameLift{gsErr: errors.New("boom")}
	p := NewGameLiftProvider(fake, "fleet-123", "")

	_, _, _, err := p.CreateGameSession(context.Background(), "q", []string{"p1"})
	require.Error(t, err)
}

func TestGameLiftProvider_ErrorWhenPsidCountMismatch(t *testing.T) {
	fake := &fakeGameLift{
		gsOut: activeGameSession(),
		psOut: &gamelift.CreatePlayerSessionsOutput{PlayerSessions: []gltypes.PlayerSession{
			{PlayerId: aws.String("p1"), PlayerSessionId: aws.String("psess-1")}, // p2 の psid 欠落
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-123", "")

	_, _, _, err := p.CreateGameSession(context.Background(), "q", []string{"p1", "p2"})
	require.Error(t, err)
}

func gamePropsToMap(props []gltypes.GameProperty) map[string]string {
	m := make(map[string]string, len(props))
	for _, p := range props {
		m[aws.ToString(p.Key)] = aws.ToString(p.Value)
	}
	return m
}

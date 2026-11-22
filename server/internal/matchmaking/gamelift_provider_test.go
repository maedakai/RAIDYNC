package matchmaking

import (
	"context"
	"errors"
	"testing"
	"time"

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
	tgIn  *gamelift.TerminateGameSessionInput
	gsOut *gamelift.CreateGameSessionOutput
	psOut *gamelift.CreatePlayerSessionsOutput
	tgOut *gamelift.TerminateGameSessionOutput
	gsErr error
	psErr error
	tgErr error

	psErrSeq []error // 設定時は CreatePlayerSessions が呼ばれるたびに順に返す（尽きたら最後の値）。nil の番は psOut を返す
	psCalls  int     // CreatePlayerSessions が呼ばれた回数
}

func (f *fakeGameLift) CreateGameSession(_ context.Context, in *gamelift.CreateGameSessionInput, _ ...func(*gamelift.Options)) (*gamelift.CreateGameSessionOutput, error) {
	f.gsIn = in
	return f.gsOut, f.gsErr
}

func (f *fakeGameLift) CreatePlayerSessions(_ context.Context, in *gamelift.CreatePlayerSessionsInput, _ ...func(*gamelift.Options)) (*gamelift.CreatePlayerSessionsOutput, error) {
	f.psIn = in
	f.psCalls++
	if len(f.psErrSeq) > 0 {
		if err := f.psErrSeq[min(f.psCalls, len(f.psErrSeq))-1]; err != nil {
			return nil, err
		}
		return f.psOut, nil
	}
	return f.psOut, f.psErr
}

func (f *fakeGameLift) TerminateGameSession(_ context.Context, in *gamelift.TerminateGameSessionInput, _ ...func(*gamelift.Options)) (*gamelift.TerminateGameSessionOutput, error) {
	f.tgIn = in
	return f.tgOut, f.tgErr
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
	p := NewGameLiftProvider(fake, "fleet-123", "custom-raidync-local", "ap-northeast-1")

	sessionID, conn, psids, err := p.CreateGameSession(context.Background(), "tok-1", "quest-rathalos", []string{"p1", "p2"})
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
	assert.Equal(t, "tok-1", aws.ToString(fake.gsIn.IdempotencyToken))
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
	p := NewGameLiftProvider(fake, "fleet-123", "", "ap-northeast-1") // location 空 = managed EC2

	_, _, _, err := p.CreateGameSession(context.Background(), "tok-1", "q", []string{"p1"})
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
	p := NewGameLiftProvider(fake, "fleet-123", "", "ap-northeast-1")

	_, _, _, err := p.CreateGameSession(context.Background(), "tok-1", "q", []string{"p1"})
	require.Error(t, err)
	assert.Nil(t, fake.psIn, "接続先未確定なら CreatePlayerSessions は呼ばない")
}

func TestGameLiftProvider_PropagatesCreateError(t *testing.T) {
	fake := &fakeGameLift{gsErr: errors.New("boom")}
	p := NewGameLiftProvider(fake, "fleet-123", "", "ap-northeast-1")

	_, _, _, err := p.CreateGameSession(context.Background(), "tok-1", "q", []string{"p1"})
	require.Error(t, err)
}

func TestGameLiftProvider_ErrorWhenPsidCountMismatch(t *testing.T) {
	fake := &fakeGameLift{
		gsOut: activeGameSession(),
		psOut: &gamelift.CreatePlayerSessionsOutput{PlayerSessions: []gltypes.PlayerSession{
			{PlayerId: aws.String("p1"), PlayerSessionId: aws.String("psess-1")}, // p2 の psid 欠落
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-123", "", "ap-northeast-1")

	_, _, _, err := p.CreateGameSession(context.Background(), "tok-1", "q", []string{"p1", "p2"})
	require.Error(t, err)
}

// TestGameLiftProvider_EndGameSession は、TerminateGameSession に完全な ARN と終わらせ方を渡すことを確認する。
func TestGameLiftProvider_EndGameSession(t *testing.T) {
	fake := &fakeGameLift{}
	p := NewGameLiftProvider(fake, "fleet-123", "custom-raidync-local", "ap-northeast-1")

	err := p.EndGameSession(context.Background(), "tok-1")
	require.NoError(t, err)

	require.NotNil(t, fake.tgIn)
	assert.Equal(t, "arn:aws:gamelift:custom-raidync-local::gamesession/fleet-123/tok-1", aws.ToString(fake.tgIn.GameSessionId))
	assert.Equal(t, gltypes.TerminationModeTriggerOnProcessTerminate, fake.tgIn.TerminationMode)
}

func TestGameLiftProvider_EndGameSessionErrors(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantErr bool
	}{
		{"not found", &gltypes.NotFoundException{}, false},
		{"invalid session", &gltypes.InvalidGameSessionStatusException{}, false},
		{"internal service", &gltypes.InternalServiceException{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeGameLift{tgErr: tc.err}
			p := NewGameLiftProvider(fake, "fleet-123", "", "ap-northeast-1")

			err := p.EndGameSession(context.Background(), "tok-1")
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestGameSessionARN(t *testing.T) {
	cases := []struct {
		name     string
		location string
		want     string
	}{
		{"anywhere", "custom-raidync-local", "arn:aws:gamelift:custom-raidync-local::gamesession/fleet-123/tok-1"},
		{"managed EC2", "", "arn:aws:gamelift:ap-northeast-1::gamesession/fleet-123/tok-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gameSessionARN(tc.location, "ap-northeast-1", "fleet-123", "tok-1")
			assert.Equal(t, tc.want, got)
		})
	}
}

func gamePropsToMap(props []gltypes.GameProperty) map[string]string {
	m := make(map[string]string, len(props))
	for _, p := range props {
		m[aws.ToString(p.Key)] = aws.ToString(p.Value)
	}
	return m
}

// TestGameLiftProvider_RetriesPlayerSessionsWhileActivating は、作った直後のゲームセッションが
// ACTIVATING の間は CreatePlayerSessions を送り直し、ACTIVE になったら psid を返すことを確認する。
func TestGameLiftProvider_RetriesPlayerSessionsWhileActivating(t *testing.T) {
	activating := &gltypes.InvalidGameSessionStatusException{Message: aws.String("Players can only join ACTIVE game sessions (status=ACTIVATING)")}
	fake := &fakeGameLift{
		gsOut:    activeGameSession(),
		psErrSeq: []error{activating, activating, nil},
		psOut: &gamelift.CreatePlayerSessionsOutput{PlayerSessions: []gltypes.PlayerSession{
			{PlayerId: aws.String("p1"), PlayerSessionId: aws.String("psess-1")},
		}},
	}
	p := NewGameLiftProvider(fake, "fleet-1", "", "ap-northeast-1")
	p.retryWait = time.Millisecond

	_, _, psids, err := p.CreateGameSession(context.Background(), "sess-1", "quest-001", []string{"p1"})

	require.NoError(t, err)
	assert.Equal(t, 3, fake.psCalls, "ACTIVATING の 2 回を送り直し、3 回目で通る")
	assert.Equal(t, map[string]string{"p1": "psess-1"}, psids)
}

// TestGameLiftProvider_GivesUpPlayerSessionsAfterMaxAttempts は、ACTIVATING が続いたら上限で諦めることを確認する。
func TestGameLiftProvider_GivesUpPlayerSessionsAfterMaxAttempts(t *testing.T) {
	activating := &gltypes.InvalidGameSessionStatusException{Message: aws.String("status=ACTIVATING")}
	fake := &fakeGameLift{gsOut: activeGameSession(), psErrSeq: []error{activating}}
	p := NewGameLiftProvider(fake, "fleet-1", "", "ap-northeast-1")
	p.retryWait = time.Millisecond

	_, _, _, err := p.CreateGameSession(context.Background(), "sess-1", "quest-001", []string{"p1"})

	assert.ErrorAs(t, err, &activating)
	assert.Equal(t, playerSessionsAttempts, fake.psCalls)
}

// TestGameLiftProvider_DoesNotRetryOtherPlayerSessionsErrors は、ACTIVATING 以外のエラーは送り直さないことを確認する。
func TestGameLiftProvider_DoesNotRetryOtherPlayerSessionsErrors(t *testing.T) {
	fake := &fakeGameLift{gsOut: activeGameSession(), psErr: errors.New("boom")}
	p := NewGameLiftProvider(fake, "fleet-1", "", "ap-northeast-1")
	p.retryWait = time.Millisecond

	_, _, _, err := p.CreateGameSession(context.Background(), "sess-1", "quest-001", []string{"p1"})

	assert.Error(t, err)
	assert.Equal(t, 1, fake.psCalls)
}

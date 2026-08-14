package matchmaking

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/gamelift"
	gltypes "github.com/aws/aws-sdk-go-v2/service/gamelift/types"

	"github.com/raidync/server/pkg/protocol"
)

// gameLiftAPI は GameLiftProvider が使う GameLift 管理 API のみを切り出したインターフェース。
// 本番は *gamelift.Client が満たし、テストはフェイクが満たす（AWS 実呼び出し無しで単体テスト可能）。
type gameLiftAPI interface {
	CreateGameSession(ctx context.Context, in *gamelift.CreateGameSessionInput, optFns ...func(*gamelift.Options)) (*gamelift.CreateGameSessionOutput, error)
	CreatePlayerSessions(ctx context.Context, in *gamelift.CreatePlayerSessionsInput, optFns ...func(*gamelift.Options)) (*gamelift.CreatePlayerSessionsOutput, error)
}

// 実 SDK クライアントが gameLiftAPI を満たすことをコンパイル時に保証する。
var _ gameLiftAPI = (*gamelift.Client)(nil)

// GameLiftProvider は実 GameLift でゲームセッションとプレイヤーセッションを作る SessionProvider。
// GameLiftStub / hubSessionProvider の本番差し替え先（G2）。
type GameLiftProvider struct {
	api        gameLiftAPI
	fleetID    string
	location   string // Anywhere のカスタムロケーション（"custom-..."）。managed EC2 では空
	maxPlayers int32  // セッション定員（RAIDYNC は 4）
}

// SessionProvider を満たすことをコンパイル時に保証する。
var _ SessionProvider = (*GameLiftProvider)(nil)

// NewGameLiftProvider は注入済み API クライアントで Provider を生成する（テスト/DI 用）。
// location が空でなければ CreateGameSession に付与する（Anywhere 用）。
func NewGameLiftProvider(api gameLiftAPI, fleetID, location string) *GameLiftProvider {
	return &GameLiftProvider{api: api, fleetID: fleetID, location: location, maxPlayers: protocol.MaxPartySize}
}

// NewGameLiftProviderFromConfig は aws.Config から実 SDK クライアントを組んで Provider を生成する。
func NewGameLiftProviderFromConfig(cfg aws.Config, fleetID, location string) *GameLiftProvider {
	return NewGameLiftProvider(gamelift.NewFromConfig(cfg), fleetID, location)
}

// CreateGameSession は GameLift に (1) ゲームセッションを 1 つ作り、(2) プレイヤー毎に
// プレイヤーセッションを作って psid を得る。返す conn は全員共通の接続先（psid 空）、
// playerSessions は playerId→psid。
//
// RAIDYNC は FlexMatch を使わないため、roster/questId は GameProperties で受け渡す
// （ゲームサーバの parseSessionConfig が読む）。psid はセッション作成後に発行されるため
// GameProperties には含めず、各クライアントへ match/status 経由で配って UDP 接続時に提示させる。
func (p *GameLiftProvider) CreateGameSession(ctx context.Context, questID string, playerIDs []string) (string, protocol.ConnectionInfo, map[string]string, error) {
	var zero protocol.ConnectionInfo

	out, err := p.api.CreateGameSession(ctx, &gamelift.CreateGameSessionInput{
		FleetId:                   aws.String(p.fleetID),
		MaximumPlayerSessionCount: aws.Int32(p.maxPlayers),
		Location:                  locationPtr(p.location),
		GameProperties: []gltypes.GameProperty{
			{Key: aws.String("questId"), Value: aws.String(questID)},
			{Key: aws.String("playerIds"), Value: aws.String(strings.Join(playerIDs, ","))},
		},
	})
	if err != nil {
		return "", zero, nil, fmt.Errorf("CreateGameSession: %w", err)
	}
	gs := out.GameSession
	if gs == nil || gs.GameSessionId == nil {
		return "", zero, nil, fmt.Errorf("CreateGameSession: 空のセッションが返った")
	}
	rawSessionID := *gs.GameSessionId
	// Anywhere は登録済み compute の IP/Port が即座に付く。managed EC2 で ACTIVATING の
	// 場合は IP 未確定になり得るため、その時は呼び出し側に判断を委ねてエラーにする。
	if gs.IpAddress == nil || gs.Port == nil {
		return "", zero, nil, fmt.Errorf("CreateGameSession: 接続先未確定 (session=%s status=%s)", rawSessionID, gs.Status)
	}
	conn := protocol.ConnectionInfo{IPAddress: *gs.IpAddress, Port: int(*gs.Port)}

	// CreatePlayerSessions は GameSessionId として ARN またはショート ID の両方を受け入れる。
	// ここでは GameLift API が返した生の値をそのまま渡す（確実に有効なため）。
	ps, err := p.api.CreatePlayerSessions(ctx, &gamelift.CreatePlayerSessionsInput{
		GameSessionId: aws.String(rawSessionID),
		PlayerIds:     playerIDs,
	})
	if err != nil {
		return "", zero, nil, fmt.Errorf("CreatePlayerSessions: %w", err)
	}
	playerSessions := make(map[string]string, len(ps.PlayerSessions))
	for _, s := range ps.PlayerSessions {
		if s.PlayerId != nil && s.PlayerSessionId != nil {
			playerSessions[*s.PlayerId] = *s.PlayerSessionId
		}
	}
	if len(playerSessions) != len(playerIDs) {
		return "", zero, nil, fmt.Errorf("CreatePlayerSessions: psid 数が不足 (want=%d got=%d)", len(playerIDs), len(playerSessions))
	}
	// GameLift は GameSessionId を ARN で返す（例: arn:aws:gamelift:...:gamesession/fleet-xxx/gsess-xxx）。
	// ARN をそのまま HTTP パスやDynamoDB PKに使うとスラッシュがセパレータとして扱われて壊れるため、
	// 末尾のショート ID（gsess-xxx）に正規化する。ゲームサーバ側(gamelift.go)も同じ正規化を行い、
	// Hub のルーティングキーと一致させる。
	sessionID := protocol.NormalizeGameSessionID(rawSessionID)
	return sessionID, conn, playerSessions, nil
}

// locationPtr は空文字なら nil を返す（managed EC2 では Location を省略する）。
func locationPtr(loc string) *string {
	if loc == "" {
		return nil
	}
	return aws.String(loc)
}

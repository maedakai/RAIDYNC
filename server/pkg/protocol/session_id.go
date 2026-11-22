package protocol

import "strings"

// NormalizeGameSessionID は GameLift の GameSessionId（ARN 形式の場合あり）から
// ショート ID を抽出して返す。既にショート形式なら変更しない。
//
// GameLift の CreateGameSession は GameSessionId として ARN を返す:
//
//	arn:aws:gamelift:ap-northeast-1::gamesession/fleet-xxx/<IdempotencyToken>
//
// このまま HTTP パスに埋めると /session/arn:aws:gamelift:.../end のようにスラッシュで
// パスが分断され、API Gateway がルート不一致で 403 を返す。ARN 末尾の ID を
// セッション ID として統一する。RAIDYNC は IdempotencyToken にマッチ確保時のトークンを渡すので、
// 末尾の ID はそのトークンと同じになる。
//
// ローカル（hubSessionProvider）はトークンをそのまま ID にし、スラッシュを含まないためそのまま通る。
func NormalizeGameSessionID(id string) string {
	// ARN 形式: arn:aws:gamelift:<region>::gamesession/<fleet-id>/<session-id>
	// ショート ID はスラッシュを含まないため、スラッシュが無ければそのまま返す。
	i := strings.LastIndex(id, "/")
	if i < 0 {
		return id
	}
	return id[i+1:]
}

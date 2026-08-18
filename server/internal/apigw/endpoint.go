package apigw

import (
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
)

// RegionEndpoint は「以降のリクエストを送るべき、リージョンに固定された API のベース URL」を返す。
//
// なぜ必要か（セッションアフィニティ）:
//
//	MatchmakingTickets は region-local（Global Tables に載せられない。楽観ロックが
//	結果整合のマルチライターで壊れるため）。したがって match/status・match/cancel は
//	チケットを作ったのと同じリージョンに届かなければ 404 になる。
//	Route 53 のレイテンシールーティングは DNS の TTL 切れやヘルスチェックの変動で
//	途中から別リージョンを返し得るので、クライアントに「以降はここへ」と明示的に
//	固定先を渡す必要がある。
//
// 解決の優先順位:
//  1. 環境変数 API_BASE_URL … Route 53 導入後に、リージョン固有のカスタムドメイン
//     （例 https://apne1.api.example.com/dev）を Terraform から注入する。
//     レイテンシールーティング対象の共通名ではなく、必ずリージョン固有名を入れること。
//  2. リクエストの requestContext から導出 … execute-api のドメインは
//     <api-id>.execute-api.<region>.amazonaws.com で元々リージョン固有なので、
//     カスタムドメイン導入前はこれで十分に固定先として機能する。
//
// 2 の方式にしているのは循環依存を避けるため。API Gateway は Lambda の ARN を必要とし
// （api_gateway モジュールが lambda モジュールに依存）、逆に Lambda が API の URL を
// 環境変数で受け取ろうとすると Terraform 上で循環する。リクエストから読めば依存が要らない。
//
// 導出できない場合は空文字を返す（クライアント側は空なら従来どおりの URL を使い続ける）。
func RegionEndpoint(req events.APIGatewayProxyRequest) string {
	if v := strings.TrimRight(os.Getenv("API_BASE_URL"), "/"); v != "" {
		return v
	}

	domain := req.RequestContext.DomainName
	if domain == "" {
		return ""
	}
	url := "https://" + domain
	if stage := req.RequestContext.Stage; stage != "" {
		url += "/" + stage
	}
	return url
}

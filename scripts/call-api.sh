#!/usr/bin/env bash
# AWS dev に上がっている API を叩く開発用スクリプト。
#
# Cognito の USER_PASSWORD_AUTH で ID トークンを取り、Authorizer 付きの
# エンドポイントへリクエストを投げる。terraform output から接続先と認証情報を
# 引くため、値をハードコードしない（環境を作り直しても追従する）。
#
# 使い方:
#   ./scripts/call-api.sh                                        # POST /match/request（東京）
#   METHOD=GET ./scripts/call-api.sh match/status/tkt-xxxx       # マッチ状況のポーリング
#   METHOD=DELETE ./scripts/call-api.sh match/cancel/tkt-xxxx    # チケットのキャンセル
#   USER_EMAIL=player2@example.com ./scripts/call-api.sh
#   TARGET=secondary ./scripts/call-api.sh                       # us-east-1 の API を叩く
#
# ⚠ チケット ID は**ボディではなくパス**に入れる（GET /match/status/{ticketId}）。
#   ボディを送るのは POST のときだけで、GET / DELETE では付けない。
#
# ⚠ マッチングの判定は match/status の中でしか動かない（matchmaking.Matcher.TryMatch）。
#   定期実行のワーカーは存在しないので、request を投げただけでは成立も TIMEOUT も
#   起こらず、チケットは TTL で静かに消える。
#
# ⚠ match/request と match/status・match/cancel は**同じ TARGET** で投げること。
#   MatchmakingTickets は region-local（Global Tables にしていない）ので、発行した
#   リージョン以外からはチケットが見えず not found になる。match/request の
#   レスポンスに入っている endpoint が、そのチケットの正しい接続先。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_DIR="$REPO_ROOT/terraform/environments/dev"

PATH_SUFFIX="${1:-match/request}"
# HTTP メソッド。エンドポイントごとに固定されている（modules/api_gateway の
# locals.endpoints が正）。TARGET と同じく、タイポが静かに通らないよう 3 択に絞る。
#   POST   /match/request, /session/{sessionId}/end
#   GET    /match/status/{ticketId}, /player/{playerId}/history
#   DELETE /match/cancel/{ticketId}
METHOD="${METHOD:-POST}"
case "$METHOD" in
  POST | GET | DELETE) ;;
  *) echo "METHOD must be POST, GET or DELETE (got: $METHOD)" >&2; exit 1 ;;
esac
# 既定ボディはブレースを含むため、${2:-...} に直書きせず変数経由で渡す
# （直書きすると内側の } が展開の終端と解釈され、末尾に余分な } が付く）。
DEFAULT_BODY='{"questId":"quest-001"}'
BODY="${2:-$DEFAULT_BODY}"
USER_EMAIL="${USER_EMAIL:-player1@example.com}"
# Cognito の認証先。**TARGET とは独立で、常に東京**であることに注意。
# ユーザープールはリージョンをまたいで複製できないため東京の 1 プールを両リージョンで
# 共用しており、ここを us-east-1 にするとプールが存在せず認証そのものが失敗する。
AUTH_REGION="${AUTH_REGION:-ap-northeast-1}"

tfout() { terraform -chdir="$ENV_DIR" output -raw "$1"; }

CLIENT=$(tfout cognito_client_id)

TARGET="${TARGET:-primary}"
case "$TARGET" in
  primary)   API_OUTPUT="api_invoke_url" ;;
  secondary) API_OUTPUT="api_invoke_url_secondary" ;;
  *) echo "TARGET must be primary or secondary (got: $TARGET)" >&2; exit 1 ;;
esac
API=$(tfout "$API_OUTPUT")
# パスワードは変数に入れるだけで、標準出力には一切出さない。
PASS=$(tfout cognito_test_user_password)

TOKEN=$(aws cognito-idp initiate-auth \
  --region "$AUTH_REGION" \
  --auth-flow USER_PASSWORD_AUTH \
  --client-id "$CLIENT" \
  --auth-parameters "USERNAME=${USER_EMAIL},PASSWORD=${PASS}" \
  --query 'AuthenticationResult.IdToken' --output text)

echo "=== ${METHOD} ${API}/${PATH_SUFFIX}  (user=${USER_EMAIL}) (target=${TARGET}) ==="
# Authorizer は "Bearer <token>" 形式を要求する（cmd/lambda/authorizer の extractBearer）。
RESP_BODY="$(mktemp)"
trap 'rm -f "$RESP_BODY"' EXIT
# ボディは POST のときだけ付ける。GET / DELETE に -d を渡すと「ボディ付き GET」になり、
# 送っても無視されるうえに意図が読めなくなる。
CURL_ARGS=(-sS -o "$RESP_BODY" -w 'HTTP %{http_code}\n'
  -X "$METHOD" "${API}/${PATH_SUFFIX}"
  -H "Authorization: Bearer ${TOKEN}")
if [ "$METHOD" = "POST" ]; then
  CURL_ARGS+=(-H "Content-Type: application/json" -d "${BODY}")
fi
curl "${CURL_ARGS[@]}"
cat "$RESP_BODY"; echo

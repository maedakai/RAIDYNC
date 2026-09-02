#!/usr/bin/env bash
# AWS dev に上がっている API を叩く開発用スクリプト。
#
# Cognito の USER_PASSWORD_AUTH で ID トークンを取り、Authorizer 付きの
# エンドポイントへリクエストを投げる。terraform output から接続先と認証情報を
# 引くため、値をハードコードしない（環境を作り直しても追従する）。
#
# 使い方:
#   ./scripts/call-api.sh                                   # POST /match/request
#   ./scripts/call-api.sh match/status '{"ticketId":"..."}'  # 任意のパス/ボディ
#   USER_EMAIL=player2@example.com ./scripts/call-api.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ENV_DIR="$REPO_ROOT/terraform/environments/dev"

PATH_SUFFIX="${1:-match/request}"
# 既定ボディはブレースを含むため、${2:-...} に直書きせず変数経由で渡す
# （直書きすると内側の } が展開の終端と解釈され、末尾に余分な } が付く）。
DEFAULT_BODY='{"questId":"quest-001"}'
BODY="${2:-$DEFAULT_BODY}"
USER_EMAIL="${USER_EMAIL:-player1@example.com}"
REGION="${REGION:-ap-northeast-1}"

tfout() { terraform -chdir="$ENV_DIR" output -raw "$1"; }

CLIENT=$(tfout cognito_client_id)
API=$(tfout api_invoke_url)
# パスワードは変数に入れるだけで、標準出力には一切出さない。
PASS=$(tfout cognito_test_user_password)

TOKEN=$(aws cognito-idp initiate-auth \
  --region "$REGION" \
  --auth-flow USER_PASSWORD_AUTH \
  --client-id "$CLIENT" \
  --auth-parameters "USERNAME=${USER_EMAIL},PASSWORD=${PASS}" \
  --query 'AuthenticationResult.IdToken' --output text)

echo "=== POST ${API}/${PATH_SUFFIX}  (user=${USER_EMAIL}) ==="
# Authorizer は "Bearer <token>" 形式を要求する（cmd/lambda/authorizer の extractBearer）。
RESP_BODY="$(mktemp)"
trap 'rm -f "$RESP_BODY"' EXIT
curl -sS -o "$RESP_BODY" -w 'HTTP %{http_code}\n' -X POST "${API}/${PATH_SUFFIX}" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "Content-Type: application/json" \
  -d "${BODY}"
cat "$RESP_BODY"; echo

#!/usr/bin/env bash
# 負荷テスト（tests/load/load-test-large.js）が Cognito に作ったテストユーザーを一掃するスクリプト。
#
# k6 は teardown() と setup() の失敗時に自分で作ったユーザーを消すが、ネットワーク断や
# k6 の強制終了（Ctrl+C）ではどちらも動かずユーザーが残る。その残りをここで消す。
# 対象はメールアドレスが PREFIX で始まるユーザーだけで、一覧を表示して確認してから消す。
#
# 使い方:
#   ./scripts/cleanup-load-test-users.sh
#
# ⚠ PREFIX は load-test-large.js の PREFIX と同じ値にすること。食い違うと k6 のユーザーが
#   一覧に出ず、何も消えない。Terraform 管理の player1〜4@example.com に当たらない値にする。
#
# ⚠ Windows の AWS CLI は --output text の行末を CRLF で出す。\r が残るとユーザー名の形式に
#   合わず admin-delete-user が失敗するため、受け取る側で tr -d '\r' で消している。
set -euo pipefail

POOL_ID='ap-northeast-1_u6Y87N8S1'
# Cognito の --filter は完全一致（=）と前方一致（^=）だけなので、固定の文字列はメールの先頭に置く。
PREFIX='k6run+'
# 一覧を取得
LIST=$(aws cognito-idp list-users \
  --region ap-northeast-1 \
  --user-pool-id "$POOL_ID" \
  --filter "email ^= \"$PREFIX\"" \
  --query 'Users[].Attributes[?Name == `email`].Value' \
  --output text | tr -d '\r')
# ０人なら終了、一覧を表示
echo "=== Found users to delete ==="
if [ -z "$LIST" ]; then
  echo "No users found."
  exit 0
fi
echo "$LIST"
read -r -p "削除しますか？ [y/N] " ANSWER
if [ "$ANSWER" != "y" ]; then
  echo "中止しました"
  exit 0
fi
# 削除
for EMAIL in $LIST; do
  echo "Deleting user: $EMAIL"
  aws cognito-idp admin-delete-user \
    --region ap-northeast-1 \
    --user-pool-id "$POOL_ID" \
    --username "$EMAIL"
done
echo "=== All users deleted ==="

#!/usr/bin/env bash
# 負荷テスト（tests/load/load-test-large.js）が使うテストユーザーを Cognito に作るスクリプト。
#
# Cognito は月のアクティブユーザー（MAU）で課金され、作成・パスワード変更・ログインのどれでも数に入る。
# k6 の実行ごとに作り直すと実行回数ぶん MAU が増えるため、ユーザーは一度だけ作って残し、
# k6 はログインだけする。何回テストしても、月の MAU は作った人数で済む。
#
# 何回実行しても同じ状態にそろう（冪等）。すでにいるユーザーは作成を飛ばし、パスワードの設定だけやり直す。
# 作成後にパスワードの設定で止まり、仮のパスワード（FORCE_CHANGE_PASSWORD）のまま残った人も次の実行で直る。
#
# 使い方:
#   export COGNITO_USER_POOL_ID=$(terraform -chdir=terraform/environments/dev output -raw cognito_user_pool_id)
#   export LOAD_TEST_PASSWORD='<プールのポリシーを満たすパスワード>'   # リポジトリには書かない
#   ./scripts/create-load-test-users.sh [人数]   # 省略時は 4 人。k6 の VUS 以上にする
#
# ⚠ PREFIX は load-test-large.js と同じ値、LOAD_TEST_PASSWORD は k6 に渡す値と同じにすること。消すときは
#   scripts/cleanup-load-test-users.sh を使う（同じ PREFIX の前方一致で選ぶ）。
set -euo pipefail

POOL_ID="${COGNITO_USER_POOL_ID:?COGNITO_USER_POOL_ID を設定してください（terraform output -raw cognito_user_pool_id）}"
# Cognito の --filter は完全一致（=）と前方一致（^=）だけなので、固定の文字列はメールの先頭に置く。
PREFIX='k6run+'
PASSWORD="${LOAD_TEST_PASSWORD:?LOAD_TEST_PASSWORD を設定してください}"
COUNT="${1:-4}"

for ((i=0; i<$COUNT; i++)); do
    EMAIL="$PREFIX$i@example.com"
    # いなければ作る。if の条件にしたコマンドは、失敗しても set -e で止まらない
    if ! aws cognito-idp admin-get-user \
        --user-pool-id "$POOL_ID" \
        --username "$EMAIL" \
        --region ap-northeast-1 > /dev/null 2>&1; then
        aws cognito-idp admin-create-user \
            --user-pool-id "$POOL_ID" \
            --username "$EMAIL" \
            --user-attributes Name=email,Value="$EMAIL" Name=email_verified,Value=true \
            --message-action SUPPRESS \
            --region ap-northeast-1 > /dev/null
        echo "created: $EMAIL"
    else
        echo "exists: $EMAIL"
    fi
    # 全員に設定し直す。Permanent にしないと仮のパスワード扱いになり、ログイン時に変更を求められる
    aws cognito-idp admin-set-user-password \
        --user-pool-id "$POOL_ID" \
        --username "$EMAIL" \
        --password "$PASSWORD" \
        --permanent \
        --region ap-northeast-1 > /dev/null
    echo "password set: $EMAIL"
done

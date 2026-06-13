#!/usr/bin/env bash
# RAIDYNC LocalStack 初期化スクリプト
#
# `docker compose up -d` で LocalStack を起動した後に実行する。
# CLAUDE.md 仕様の 3 つの DynamoDB テーブル
#   - Players       (PK: playerId)
#   - Sessions      (PK: sessionId)
#   - MatchHistory  (PK: playerId, SK: timestamp)
# を冪等に作成する（既に存在する場合はスキップ）。
#
# 実行例:
#   bash scripts/localstack-init.sh
#   ./scripts/localstack-init.sh   # 実行権限があれば

set -euo pipefail

# ----- 設定 -----------------------------------------------------------------
ENDPOINT_URL="${LOCALSTACK_ENDPOINT:-http://localhost:4566}"
REGION="${AWS_DEFAULT_REGION:-ap-northeast-1}"

# LocalStack はダミー認証情報で動くので未設定時のフォールバックを用意
export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-test}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-test}"
export AWS_DEFAULT_REGION="$REGION"

# ----- ユーティリティ -------------------------------------------------------
aws_cli() {
  aws --endpoint-url="$ENDPOINT_URL" --region "$REGION" "$@"
}

log()  { echo "[init] $*"; }
fail() { echo "[init][ERROR] $*" >&2; exit 1; }

wait_for_localstack() {
  log "LocalStack の応答確認 ($ENDPOINT_URL) ..."
  for _ in $(seq 1 30); do
    if curl -sf "$ENDPOINT_URL/_localstack/health" >/dev/null 2>&1; then
      log "LocalStack 応答 OK"
      return 0
    fi
    sleep 2
  done
  fail "LocalStack に接続できません。先に 'docker compose up -d' を実行してください"
}

create_table_if_missing() {
  local table_name="$1"
  shift
  if aws_cli dynamodb describe-table --table-name "$table_name" >/dev/null 2>&1; then
    log "skip   : $table_name (既存)"
  else
    log "create : $table_name"
    aws_cli dynamodb create-table \
      --table-name "$table_name" \
      "$@" \
      --billing-mode PAY_PER_REQUEST >/dev/null
  fi
}

# ----- 本処理 ---------------------------------------------------------------
wait_for_localstack

# Players: プレイヤープロファイル
create_table_if_missing Players \
  --attribute-definitions AttributeName=playerId,AttributeType=S \
  --key-schema           AttributeName=playerId,KeyType=HASH

# Sessions: アクティブなゲームセッション状態
create_table_if_missing Sessions \
  --attribute-definitions AttributeName=sessionId,AttributeType=S \
  --key-schema           AttributeName=sessionId,KeyType=HASH

# MatchHistory: マッチ履歴（playerId × timestamp）
create_table_if_missing MatchHistory \
  --attribute-definitions \
      AttributeName=playerId,AttributeType=S \
      AttributeName=timestamp,AttributeType=N \
  --key-schema \
      AttributeName=playerId,KeyType=HASH \
      AttributeName=timestamp,KeyType=RANGE

# MatchmakingTickets: マッチ待機チケット（短命・region-local・TTL 90 秒）
#   - GSI: questId-status-index  ... 同 quest の WAITING を抽出
#   - GSI: playerId-status-index ... 1 プレイヤー 1 チケット制約
create_table_if_missing MatchmakingTickets \
  --attribute-definitions \
      AttributeName=ticketId,AttributeType=S \
      AttributeName=questId,AttributeType=S \
      AttributeName=status,AttributeType=S \
      AttributeName=playerId,AttributeType=S \
  --key-schema \
      AttributeName=ticketId,KeyType=HASH \
  --global-secondary-indexes \
      'IndexName=questId-status-index,KeySchema=[{AttributeName=questId,KeyType=HASH},{AttributeName=status,KeyType=RANGE}],Projection={ProjectionType=ALL}' \
      'IndexName=playerId-status-index,KeySchema=[{AttributeName=playerId,KeyType=HASH},{AttributeName=status,KeyType=RANGE}],Projection={ProjectionType=ALL}'

# TTL を有効化（既に有効でもエラーにしない）
log "enable TTL: MatchmakingTickets.ttl"
aws_cli dynamodb update-time-to-live \
  --table-name MatchmakingTickets \
  --time-to-live-specification "Enabled=true, AttributeName=ttl" >/dev/null 2>&1 || true

# ----- 結果サマリ -----------------------------------------------------------
log "現在のテーブル一覧:"
aws_cli dynamodb list-tables

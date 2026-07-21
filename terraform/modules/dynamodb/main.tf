# RAIDYNC の DynamoDB テーブル群を IaC 化するモジュール。
#
# スキーマの権威は docker/localstack/scripts/localstack-init.sh（ローカルと同一定義）と
# server/internal/db/*.go。テーブル名は internal/db.DefaultTables() が固定名で参照するため
# 実名（Players / Sessions / MatchHistory / MatchmakingTickets）で作る。
#
# 課金: PAY_PER_REQUEST 既定（叩かなければほぼ無料）。固定の時間課金は発生しない。

# プレイヤープロファイル（PK: playerId）
resource "aws_dynamodb_table" "players" {
  name         = "Players"
  billing_mode = var.billing_mode
  hash_key     = "playerId"

  attribute {
    name = "playerId"
    type = "S"
  }

  point_in_time_recovery {
    enabled = var.point_in_time_recovery
  }

  tags = var.tags
}

# アクティブなゲームセッション状態（PK: sessionId）
resource "aws_dynamodb_table" "sessions" {
  name         = "Sessions"
  billing_mode = var.billing_mode
  hash_key     = "sessionId"

  attribute {
    name = "sessionId"
    type = "S"
  }

  point_in_time_recovery {
    enabled = var.point_in_time_recovery
  }

  tags = var.tags
}

# マッチ結果履歴（PK: playerId, SK: timestamp 数値・降順取得）
resource "aws_dynamodb_table" "match_history" {
  name         = "MatchHistory"
  billing_mode = var.billing_mode
  hash_key     = "playerId"
  range_key    = "timestamp"

  attribute {
    name = "playerId"
    type = "S"
  }

  attribute {
    name = "timestamp"
    type = "N"
  }

  point_in_time_recovery {
    enabled = var.point_in_time_recovery
  }

  tags = var.tags
}

# マッチ待機チケット（PK: ticketId, 短命 TTL=ttl）
#   GSI questId-status-index  … 同 quest の WAITING を抽出（QueryWaitingByQuest）
#   GSI playerId-status-index … 1 プレイヤー 1 チケット制約（FindWaitingByPlayer）
resource "aws_dynamodb_table" "matchmaking_tickets" {
  name         = "MatchmakingTickets"
  billing_mode = var.billing_mode
  hash_key     = "ticketId"

  attribute {
    name = "ticketId"
    type = "S"
  }

  attribute {
    name = "questId"
    type = "S"
  }

  attribute {
    name = "status"
    type = "S"
  }

  attribute {
    name = "playerId"
    type = "S"
  }

  global_secondary_index {
    name            = "questId-status-index"
    hash_key        = "questId"
    range_key       = "status"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "playerId-status-index"
    hash_key        = "playerId"
    range_key       = "status"
    projection_type = "ALL"
  }

  # 期限切れチケットの自動削除（localstack-init と同じ属性名 ttl）。
  ttl {
    attribute_name = "ttl"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = var.point_in_time_recovery
  }

  tags = var.tags
}

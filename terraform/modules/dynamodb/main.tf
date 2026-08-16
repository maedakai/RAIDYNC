# RAIDYNC の DynamoDB テーブル群を IaC 化するモジュール。
#
# スキーマの権威は docker/localstack/scripts/localstack-init.sh（ローカルと同一定義）と
# server/internal/db/*.go。テーブル名は internal/db.DefaultTables() が固定名で参照するため
# 実名（Players / Sessions / MatchHistory / MatchmakingTickets）で作る。
#
# 課金: PAY_PER_REQUEST 既定（叩かなければほぼ無料）。固定の時間課金は発生しない。
#
# Global Tables（Phase 5）:
#   replica_regions が空でなければ Players / Sessions / MatchHistory を Global Tables 化する。
#   MatchmakingTickets は対象外（region-local。理由は variables.tf の replica_regions を参照）。
#   Terraform では replica ブロックが「レプリカテーブルの作成」までを担うため、セカンダリ
#   リージョン用の provider alias は不要（AWS 側が裏でレプリカを作る）。

locals {
  # Global Tables は DynamoDB Streams（NEW_AND_OLD_IMAGES）が前提要件。
  # レプリカを置かない構成では Streams を有効化しない（不要なコストと複雑さを避ける）。
  global_tables_enabled = length(var.replica_regions) > 0
}

# プレイヤープロファイル（PK: playerId）
resource "aws_dynamodb_table" "players" {
  name         = "Players"
  billing_mode = var.billing_mode
  hash_key     = "playerId"

  attribute {
    name = "playerId"
    type = "S"
  }

  stream_enabled   = local.global_tables_enabled
  stream_view_type = local.global_tables_enabled ? "NEW_AND_OLD_IMAGES" : null

  dynamic "replica" {
    for_each = toset(var.replica_regions)
    content {
      region_name            = replica.value
      point_in_time_recovery = var.point_in_time_recovery
      propagate_tags         = true
    }
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

  stream_enabled   = local.global_tables_enabled
  stream_view_type = local.global_tables_enabled ? "NEW_AND_OLD_IMAGES" : null

  dynamic "replica" {
    for_each = toset(var.replica_regions)
    content {
      region_name            = replica.value
      point_in_time_recovery = var.point_in_time_recovery
      propagate_tags         = true
    }
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

  stream_enabled   = local.global_tables_enabled
  stream_view_type = local.global_tables_enabled ? "NEW_AND_OLD_IMAGES" : null

  dynamic "replica" {
    for_each = toset(var.replica_regions)
    content {
      region_name            = replica.value
      point_in_time_recovery = var.point_in_time_recovery
      propagate_tags         = true
    }
  }

  point_in_time_recovery {
    enabled = var.point_in_time_recovery
  }

  tags = var.tags
}

# マッチ待機チケット（PK: ticketId, 短命 TTL=ttl）
#   GSI questId-status-index  … 同 quest の WAITING を抽出（QueryWaitingByQuest）
#   GSI playerId-status-index … 1 プレイヤー 1 チケット制約（FindWaitingByPlayer）
#
# ⚠ このテーブルは Global Tables にしない（replica ブロックを持たない唯一のテーブル）。
#   マッチメイキングは楽観ロック（version 属性）で「1 チケットを 1 回だけ MATCHING に遷移」
#   させることで成立する。Global Tables は結果整合のマルチライターであり、リージョン間で
#   同一チケットが同時に確保され得るため、この不変条件が壊れる。マッチングは region-local に
#   閉じ、リージョンをまたぐ待機列の統合は行わない（spec-architecture.md §2.5）。
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

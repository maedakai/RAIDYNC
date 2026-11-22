# RAIDYNC の**クロスリージョン共有**テーブル（Players / Sessions / MatchHistory）を
# IaC 化するモジュール。region-local な MatchmakingTickets は modules/dynamodb_tickets。
#
# スキーマの権威は docker/localstack/scripts/localstack-init.sh（ローカルと同一定義）と
# server/internal/db/*.go。テーブル名は internal/db.DefaultTables() が固定名で参照するため
# 実名（Players / Sessions / MatchHistory）で作る。
#
# 課金: PAY_PER_REQUEST 既定（叩かなければほぼ無料）。固定の時間課金は発生しない。
#   Global Tables 化してもレプリカの「存在」自体に固定費は無く、rWCU・保存量・
#   クロスリージョン転送の従量課金のみ（GameLift フリートとは性質が異なる）。
#
# Global Tables（Phase 5）:
#   replica_regions が空でなければ 3 テーブルを Global Tables 化する。
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

# 注: MatchmakingTickets はこのモジュールに含めない。
#   Global Tables に載せられない（楽観ロックが結果整合のマルチライターで壊れる）ため
#   region-local として各リージョンに独立した実体を置く必要があり、
#   modules/dynamodb_tickets に分離してある。設計理由はそちらのヘッダコメント参照。

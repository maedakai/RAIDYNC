# マッチ待機チケット（PK: ticketId, 短命 TTL=ttl）— **region-local テーブル**
#
#   GSI questId-status-index  … 同 quest の WAITING を抽出（QueryWaitingByQuest）
#   GSI playerId-status-index … 1 プレイヤー 1 チケット制約（FindWaitingByPlayer）
#
# なぜ modules/dynamodb（Global Tables）から分離しているか:
#   マッチメイキングは楽観ロック（version 属性 + 条件付き書き込み）で
#   「1 チケットを 1 回だけ WAITING→MATCHING に遷移させる」ことで成立している。
#   Global Tables は結果整合のマルチライター（last writer wins）であり、
#   リージョンをまたぐと同一チケットが同時に確保され得てこの排他が壊れる。
#
#   したがってこのテーブルはレプリケーションせず、**各リージョンに独立した実体を置く**。
#   マッチングは自リージョン内で完結し、待機列はリージョンをまたいで統合されない。
#   （同名テーブルだが DynamoDB のテーブル名はリージョンスコープなので衝突しない。）
#
#   Global Tables 側（Players / Sessions / MatchHistory）は modules/dynamodb を参照。

resource "aws_dynamodb_table" "this" {
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

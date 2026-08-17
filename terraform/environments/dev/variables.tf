variable "region" {
  description = "デプロイ先リージョン（プライマリ）。"
  type        = string
  default     = "ap-northeast-1"
}

variable "enable_gamelift" {
  description = <<-EOT
    GameLift Managed EC2 フリートを作るか。**既定 false**。

    このプロジェクトで唯一「立てているだけで時間課金される」リソースなので、
    既定では作らず、E2E 検証をするときだけ明示的に true にする
    （-var="enable_gamelift=true"）。検証が終わったら false に戻して apply すれば消える。
    CLAUDE.md「GameLift フリートは使用しないときは必ず停止すること」の実装。
  EOT
  type        = bool
  default     = false
}

variable "secondary_region" {
  description = <<-EOT
    セカンダリリージョン（Phase 5）。spec-architecture.md §リージョン構成の us-east-1（バージニア）。
    ここに region-local な MatchmakingTickets と、セカンダリの Lambda / API Gateway を置く。
  EOT
  type        = string
  default     = "us-east-1"
}

variable "replica_regions" {
  description = <<-EOT
    DynamoDB Global Tables のレプリカを置くリージョン（Phase 5）。
    通常は [var.secondary_region] と一致させる。
    空にすれば単一リージョン構成に戻せる（レプリカ削除の apply が走る）。
  EOT
  type        = list(string)
  default     = ["us-east-1"]
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default = {
    Project = "RAIDYNC"
    Env     = "dev"
    Phase   = "4"
  }
}

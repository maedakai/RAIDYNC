variable "region" {
  description = "デプロイ先リージョン（プライマリ）。"
  type        = string
  default     = "ap-northeast-1"
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

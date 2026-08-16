variable "region" {
  description = "デプロイ先リージョン（プライマリ）。"
  type        = string
  default     = "ap-northeast-1"
}

variable "replica_regions" {
  description = <<-EOT
    DynamoDB Global Tables のレプリカを置くリージョン（Phase 5）。
    spec-architecture.md のリージョン構成では us-east-1（バージニア）がセカンダリ。
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

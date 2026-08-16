variable "billing_mode" {
  description = "課金モード。dev は叩いた分だけ課金される PAY_PER_REQUEST を既定にする（固定容量課金を避ける）。"
  type        = string
  default     = "PAY_PER_REQUEST"

  validation {
    condition     = contains(["PAY_PER_REQUEST", "PROVISIONED"], var.billing_mode)
    error_message = "billing_mode は PAY_PER_REQUEST か PROVISIONED のいずれか。"
  }
}

variable "point_in_time_recovery" {
  description = "PITR（継続バックアップ）を有効化するか。dev では不要なため既定 false（コスト抑制）。"
  type        = bool
  default     = false
}

variable "replica_regions" {
  description = <<-EOT
    Global Tables のレプリカを置くリージョン一覧（プライマリ以外）。例: ["us-east-1"]。
    既定は空＝単一リージョン構成（Phase 4 までの挙動）。空でない場合のみ DynamoDB Streams が
    有効化される（Global Tables の前提要件）。

    対象は Players / Sessions / MatchHistory の 3 テーブルのみ。MatchmakingTickets は
    region-local（レプリケーションすると同一チケットが両リージョンで二重にマッチされるため）。
    spec-architecture.md §2.5 の方針。
  EOT
  type        = list(string)
  default     = []
}

variable "tags" {
  description = "全テーブルに付与する共通タグ。"
  type        = map(string)
  default     = {}
}

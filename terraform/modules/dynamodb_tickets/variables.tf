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

variable "tags" {
  description = "テーブルに付与する共通タグ。"
  type        = map(string)
  default     = {}
}

variable "domain_names" {
  description = <<-EOT
    証明書に載せるドメイン名。先頭が CN、残りが SAN になる。
    例: ["api.raidync.com", "apne1.api.raidync.com"]
    レイテンシールーティング用の共通名と、セッションアフィニティ用のリージョン固定名の両方を含めること。
  EOT
  type        = list(string)

  validation {
    condition     = length(var.domain_names) > 0
    error_message = "domain_names は 1 つ以上必要。"
  }
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

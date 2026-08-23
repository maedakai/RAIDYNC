variable "domain_names" {
  description = <<-EOT
    このリージョンに作るカスタムドメイン名。
    例: ["api.raidync.com", "apne1.api.raidync.com"]
    certificate_arn の証明書がこれら全てをカバーしている必要がある。
  EOT
  type        = list(string)
}

variable "certificate_arn" {
  description = <<-EOT
    使用する ACM 証明書 ARN。**検証済み（ISSUED）である必要がある**ため、
    呼び出し側は aws_acm_certificate_validation.certificate_arn を渡すこと
    （証明書リソースの arn を直接渡すと、検証完了前に作成が走って失敗し得る）。
    証明書はこのモジュールと同一リージョンにあること（REGIONAL の制約）。
  EOT
  type        = string
}

variable "rest_api_id" {
  description = "紐付ける REST API の ID（modules/api_gateway の rest_api_id）。"
  type        = string
}

variable "stage_name" {
  description = "紐付けるステージ名。"
  type        = string
  default     = "dev"
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

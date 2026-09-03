variable "name_prefix" {
  description = "リソース名の接頭辞。"
  type        = string
  default     = "raidync-dev"
}

variable "test_user_count" {
  description = <<-EOT
    E2E 用テストユーザーの数。既定 4 は protocol.MaxPartySize（1 セッション 4 人）に合わせている。
    ソロ経路（soloAllowed）なら 1 人でも E2E は完走できるが、4 人マッチも検証できるようにする。
  EOT
  type        = number
  default     = 4
}

variable "test_user_email_domain" {
  description = "テストユーザーのメールドメイン（実在しなくてよい。検証済み扱いで作成するため送信は発生しない）。"
  type        = string
  default     = "example.com"
}

variable "access_token_hours" {
  description = "アクセストークン有効期限（時間）。spec-architecture.md §2.3 の 1 時間。"
  type        = number
  default     = 1
}

variable "refresh_token_days" {
  description = "リフレッシュトークン有効期限（日）。spec-architecture.md §2.3 の 30 日。"
  type        = number
  default     = 30
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

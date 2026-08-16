variable "name_prefix" {
  description = "Lambda 関数名の接頭辞（<prefix>-<function> になる）。"
  type        = string
  default     = "raidync-dev"
}

variable "artifacts_dir" {
  description = <<-EOT
    scripts/build-lambdas.sh が出力した成果物ルート。
    <artifacts_dir>/<function>/bootstrap が存在している必要がある（apply 前にビルドすること）。
  EOT
  type        = string
}

variable "authorizer_role_arn" {
  description = "authorizer Lambda に割り当てる IAM ロール ARN（Logs のみ）。"
  type        = string
}

variable "matchmaking_role_arn" {
  description = "match_* / session_end Lambda に割り当てる IAM ロール ARN（DynamoDB + GameLift + Logs）。"
  type        = string
}

# ---- authorizer 用（Cognito モジュールの output を渡す） ----

variable "auth_jwks_url" {
  description = "Cognito User Pool の JWKS URL。空なら環境変数を設定しない（authorizer は起動時に必須なので apply 時には必ず渡すこと）。"
  type        = string
  default     = ""
}

variable "auth_issuer" {
  description = "トークン issuer（https://cognito-idp.<region>.amazonaws.com/<userPoolId>）。"
  type        = string
  default     = ""
}

variable "auth_client_id" {
  description = "Cognito App Client ID。空なら aud/client_id チェックを行わない（コード側仕様）。"
  type        = string
  default     = ""
}

# ---- match_status 用（GameLift モジュールの output を渡す） ----

variable "gamelift_fleet_id" {
  description = <<-EOT
    実 GameLift フリート ID。空の場合 match_status は GameLiftStub にフォールバックする
    （cmd/lambda/match_status/main.go の selectSessionProvider）。
  EOT
  type        = string
  default     = ""
}

variable "gamelift_location" {
  description = <<-EOT
    GameLift のカスタムロケーション。Anywhere 用の値であり、
    本番同等の Managed EC2 フリートでは空にすること。
  EOT
  type        = string
  default     = ""
}

# ---- 実行設定 ----

variable "memory_size" {
  description = "メモリ (MB)。spec-architecture.md §2.4 の 256MB を既定とする。"
  type        = number
  default     = 256
}

variable "timeout" {
  description = "タイムアウト (秒)。spec-architecture.md §2.4 の 30 秒を既定とする。"
  type        = number
  default     = 30
}

variable "log_retention_days" {
  description = "CloudWatch Logs の保持日数。無期限保持によるコスト増を避けるため明示する。"
  type        = number
  default     = 7
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

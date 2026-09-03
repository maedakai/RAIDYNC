variable "name_prefix" {
  description = "リソース名の接頭辞。"
  type        = string
  default     = "raidync-dev"
}

variable "stage_name" {
  description = "デプロイステージ名。invoke_url の末尾になる。"
  type        = string
  default     = "dev"
}

variable "lambda_invoke_arns" {
  description = "関数キー => invoke ARN（modules/lambda の invoke_arns をそのまま渡す）。"
  type        = map(string)
}

variable "lambda_function_names" {
  description = "関数キー => 関数名（lambda_permission の付与先に使う）。"
  type        = map(string)
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

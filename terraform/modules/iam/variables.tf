variable "dynamodb_table_arns" {
  description = <<-EOT
    matchmaking ロールに CRUD を許可する DynamoDB テーブル ARN の一覧。
    GSI は arn/index/* で併せて許可する。

    IAM ロールはグローバルリソースなので 1 つのロールが全リージョンの Lambda に使えるが、
    **テーブル ARN はリージョンごとに別物**（同名テーブルでも arn:aws:dynamodb:<region>:... が異なる）。
    マルチリージョン構成ではプライマリ・セカンダリ両方の ARN を渡すこと。
  EOT
  type        = list(string)
}

variable "tags" {
  description = "IAM リソースに付与する共通タグ。"
  type        = map(string)
  default     = {}
}

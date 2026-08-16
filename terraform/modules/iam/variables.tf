variable "dynamodb_table_arns" {
  description = "matchmaking ロールに CRUD を許可する DynamoDB テーブル ARN（論理名 => arn）。GSI は arn/index/* で併せて許可する。"
  type        = map(string)
}

variable "tags" {
  description = "IAM リソースに付与する共通タグ。"
  type        = map(string)
  default     = {}
}

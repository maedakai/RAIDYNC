output "table_name" {
  description = "実テーブル名（internal/db.DefaultTables() が固定名で参照する）。"
  value       = aws_dynamodb_table.this.name
}

output "table_arn" {
  description = "テーブル ARN（IAM 最小権限ポリシーで使用）。GSI を含めるには /index/* を付けること。"
  value       = aws_dynamodb_table.this.arn
}

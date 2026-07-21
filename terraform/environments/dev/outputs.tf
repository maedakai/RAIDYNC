output "dynamodb_table_names" {
  description = "DynamoDB 論理名 => 実テーブル名。"
  value       = module.dynamodb.table_names
}

output "dynamodb_table_arns" {
  description = "DynamoDB 論理名 => テーブル ARN。"
  value       = module.dynamodb.table_arns
}

output "dynamodb_table_names" {
  description = "DynamoDB 論理名 => 実テーブル名。"
  value       = module.dynamodb.table_names
}

output "dynamodb_table_arns" {
  description = "DynamoDB 論理名 => テーブル ARN。"
  value       = module.dynamodb.table_arns
}

output "lambda_function_names" {
  description = "Lambda 関数キー => 関数名。"
  value       = module.lambda.function_names
}

output "api_invoke_url" {
  description = "API のベース URL。E2E クライアントの接続先。"
  value       = module.api_gateway.invoke_url
}

output "cognito_user_pool_id" {
  description = "Cognito User Pool ID。"
  value       = module.cognito.user_pool_id
}

output "cognito_client_id" {
  description = "App Client ID。E2E の InitiateAuth に使う。"
  value       = module.cognito.client_id
}

output "cognito_test_user_emails" {
  description = "E2E テストユーザーのログイン ID 一覧。"
  value       = module.cognito.test_user_emails
}

output "cognito_test_user_password" {
  description = "E2E テストユーザーの共通パスワード。取得は terraform output -raw cognito_test_user_password。"
  value       = module.cognito.test_user_password
  sensitive   = true
}

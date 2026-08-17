output "dynamodb_table_names" {
  description = "DynamoDB 論理名 => 実テーブル名（Tickets を含む全テーブル）。"
  value = merge(
    module.dynamodb.table_names,
    { matchmaking_tickets = module.dynamodb_tickets.table_name },
  )
}

output "dynamodb_table_arns" {
  description = "DynamoDB 論理名 => テーブル ARN（プライマリリージョン）。"
  value = merge(
    module.dynamodb.table_arns,
    { matchmaking_tickets = module.dynamodb_tickets.table_arn },
  )
}

output "dynamodb_global_tables" {
  description = "Global Tables 化したテーブルとレプリカ配置先リージョン。"
  value       = module.dynamodb.global_tables
}

output "dynamodb_tickets_secondary_arn" {
  description = "セカンダリリージョンの MatchmakingTickets ARN（region-local。Global Tables ではない）。"
  value       = module.dynamodb_tickets_secondary.table_arn
}

output "lambda_function_names" {
  description = "Lambda 関数キー => 関数名（プライマリリージョン）。"
  value       = module.lambda.function_names
}

output "gamelift_fleet_id" {
  description = "Managed EC2 フリート ID。enable_gamelift=false のときは null。"
  value       = one(module.gamelift_ec2[*].fleet_id)
}

output "gamelift_build_id" {
  description = "GameLift Build ID。enable_gamelift=false のときは null。"
  value       = one(module.gamelift_ec2[*].build_id)
}

output "api_invoke_url" {
  description = "プライマリ（ap-northeast-1）API のベース URL。E2E クライアントの接続先。"
  value       = module.api_gateway.invoke_url
}

output "api_invoke_url_secondary" {
  description = "セカンダリ（us-east-1）API のベース URL。マルチリージョン検証で直接叩く先。"
  value       = module.api_gateway_secondary.invoke_url
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

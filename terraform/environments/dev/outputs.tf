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

output "gamelift_fleet_ids" {
  description = <<-EOT
    リージョン => Managed EC2 フリート ID。enable_gamelift=false なら空。
    ⚠ フリートは唯一の時間課金リソース。検証が終わったら必ず
    enable_gamelift=false で apply して消すこと（2 基で約 $0.06/時）。
  EOT
  value = var.enable_gamelift ? {
    (var.region)           = module.gamelift_ec2[0].fleet_id
    (var.secondary_region) = module.gamelift_ec2_secondary[0].fleet_id
  } : {}
}

output "gamelift_build_ids" {
  description = "リージョン => GameLift Build ID。enable_gamelift=false なら空。"
  value = var.enable_gamelift ? {
    (var.region)           = module.gamelift_ec2[0].build_id
    (var.secondary_region) = module.gamelift_ec2_secondary[0].build_id
  } : {}
}

output "api_invoke_url" {
  description = "プライマリ（ap-northeast-1）API のベース URL。E2E クライアントの接続先。"
  value       = module.api_gateway.invoke_url
}

output "api_invoke_url_secondary" {
  description = "セカンダリ（us-east-1）API のベース URL。マルチリージョン検証で直接叩く先。"
  value       = module.api_gateway_secondary.invoke_url
}

output "api_custom_domains" {
  description = <<-EOT
    カスタムドメイン名。domain_name 未設定なら空。
      latency   … レイテンシールーティング対象（クライアントの入口）
      primary   … 東京固定（match/request が API_BASE_URL として返す）
      secondary … バージニア固定（同上）
  EOT
  value = local.dns_enabled ? {
    latency   = local.api_fqdn
    primary   = local.api_fqdn_primary
    secondary = local.api_fqdn_secondary
  } : {}
}

output "api_health_check_ids" {
  description = "リージョン => Route 53 ヘルスチェック ID。enable_health_checks=false なら空。"
  value       = { for k, h in aws_route53_health_check.api : k => h.id }
}

output "api_regional_endpoints" {
  description = <<-EOT
    リージョン => (カスタムドメイン名 => API Gateway のリージョナルエンドポイント)。
    Route 53 alias レコードの向き先に使う。

    リージョンで階層を分けているのは、レイテンシールーティング用の共通名
    api.<domain> が**両リージョンに同名で存在する**ため。1 段の map に merge すると
    キーが衝突して片方が消える。
  EOT
  value = local.dns_enabled ? {
    (var.region)           = module.api_domain[0].regional_endpoints
    (var.secondary_region) = module.api_domain_secondary[0].regional_endpoints
  } : {}
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

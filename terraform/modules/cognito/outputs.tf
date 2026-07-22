# authorizer Lambda の環境変数（AUTH_*）と、E2E のログインに使う。

output "user_pool_id" {
  description = "Cognito User Pool ID。"
  value       = aws_cognito_user_pool.this.id
}

output "client_id" {
  description = "App Client ID。authorizer の AUTH_CLIENT_ID、および InitiateAuth に使う。"
  value       = aws_cognito_user_pool_client.this.id
}

output "issuer" {
  description = "トークン issuer。authorizer の AUTH_ISSUER に渡す。"
  value       = "https://cognito-idp.${data.aws_region.current.name}.amazonaws.com/${aws_cognito_user_pool.this.id}"
}

output "jwks_url" {
  description = "JWKS エンドポイント。authorizer の AUTH_JWKS_URL に渡す。"
  value       = "https://cognito-idp.${data.aws_region.current.name}.amazonaws.com/${aws_cognito_user_pool.this.id}/.well-known/jwks.json"
}

output "test_user_emails" {
  description = "E2E テストユーザーのメールアドレス一覧（= ログイン ID）。"
  value       = aws_cognito_user.test[*].username
}

output "test_user_password" {
  description = "E2E テストユーザーの共通パスワード。取得は terraform output -raw。"
  value       = random_password.test_user.result
  sensitive   = true
}

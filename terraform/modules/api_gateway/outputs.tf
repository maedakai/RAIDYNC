output "invoke_url" {
  description = "API のベース URL。E2E クライアントの接続先（ローカル E2E の http://localhost:8080 に相当）。"
  value       = aws_api_gateway_stage.this.invoke_url
}

output "rest_api_id" {
  description = "REST API ID。"
  value       = aws_api_gateway_rest_api.this.id
}

output "execution_arn" {
  description = "execute-api の実行 ARN（追加の権限付与が必要になった場合に使う）。"
  value       = aws_api_gateway_rest_api.this.execution_arn
}

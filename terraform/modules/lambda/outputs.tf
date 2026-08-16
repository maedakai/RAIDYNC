# API Gateway モジュールが統合設定に使う（invoke_arn）ほか、運用時の参照用。

output "function_names" {
  description = "関数キー => Lambda 関数名。"
  value       = { for k, fn in aws_lambda_function.this : k => fn.function_name }
}

output "function_arns" {
  description = "関数キー => Lambda 関数 ARN（API Gateway の権限付与に使用）。"
  value       = { for k, fn in aws_lambda_function.this : k => fn.arn }
}

output "invoke_arns" {
  description = "関数キー => invoke ARN（API Gateway の AWS_PROXY 統合 uri に使用）。"
  value       = { for k, fn in aws_lambda_function.this : k => fn.invoke_arn }
}

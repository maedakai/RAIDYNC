# Lambda モジュールが各関数にアタッチするためにロール ARN を公開する。

output "authorizer_role_arn" {
  description = "authorizer Lambda 用ロール ARN（Logs のみ）。"
  value       = aws_iam_role.authorizer.arn
}

output "matchmaking_role_arn" {
  description = "match_* / session_end Lambda 用ロール ARN（DynamoDB + GameLift + Logs）。"
  value       = aws_iam_role.matchmaking.arn
}

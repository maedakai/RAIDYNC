# Lambda の環境変数（テーブル名）や IAM ポリシー（テーブル ARN）で参照するため公開する。

output "table_names" {
  description = "論理名 => 実テーブル名。"
  value = {
    players             = aws_dynamodb_table.players.name
    sessions            = aws_dynamodb_table.sessions.name
    match_history       = aws_dynamodb_table.match_history.name
    matchmaking_tickets = aws_dynamodb_table.matchmaking_tickets.name
  }
}

output "table_arns" {
  description = "論理名 => テーブル ARN（IAM 最小権限ポリシーで使用）。GSI も含めるため /index/* を付けた ARN も併記。"
  value = {
    players             = aws_dynamodb_table.players.arn
    sessions            = aws_dynamodb_table.sessions.arn
    match_history       = aws_dynamodb_table.match_history.arn
    matchmaking_tickets = aws_dynamodb_table.matchmaking_tickets.arn
  }
}

# Lambda の環境変数（テーブル名）や IAM ポリシー（テーブル ARN）で参照するため公開する。

output "table_names" {
  description = "論理名 => 実テーブル名。"
  value = {
    players       = aws_dynamodb_table.players.name
    sessions      = aws_dynamodb_table.sessions.name
    match_history = aws_dynamodb_table.match_history.name
  }
}

output "table_arns" {
  description = "論理名 => テーブル ARN（プライマリリージョン）。IAM 最小権限ポリシーで使用。GSI を含めるには /index/* を付けること。"
  value = {
    players       = aws_dynamodb_table.players.arn
    sessions      = aws_dynamodb_table.sessions.arn
    match_history = aws_dynamodb_table.match_history.arn
  }
}

output "replica_table_arns" {
  description = <<-EOT
    レプリカテーブルの ARN 一覧（セカンダリリージョン）。
    同名テーブルでもリージョンが違えば ARN が別物なので、セカンダリの Lambda に
    IAM 許可を出すにはこちらも Resource に含める必要がある。
  EOT
  value = flatten([
    for t in [aws_dynamodb_table.players, aws_dynamodb_table.sessions, aws_dynamodb_table.match_history] :
    [for r in t.replica : r.arn]
  ])
}

output "global_tables" {
  description = "Global Tables 化したテーブルと、そのレプリカ配置先リージョン（検証・ドキュメント用）。"
  value = local.global_tables_enabled ? {
    for name in ["Players", "Sessions", "MatchHistory"] : name => var.replica_regions
  } : {}
}

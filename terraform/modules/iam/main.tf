# Lambda 実行ロールを用途別に 2 つ定義する。
#   - authorizer_role : JWT 検証のみ（DynamoDB/GameLift に触れない）→ Logs 権限だけ
#   - matchmaking_role: match_* / session_end 用。DynamoDB CRUD + GameLift セッション作成 + Logs
# dev E2E の狙いのひとつが「最小権限で本当に動くか」の実効性検証なので、role を分ける。

# Lambda サービスが assume できることを示す信頼ポリシー（両ロール共通）。
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

# CloudWatch Logs への出力は AWS 標準のマネージドポリシーで賄う。
locals {
  lambda_basic_exec = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# ---- authorizer 用ロール（Logs のみ） ----
resource "aws_iam_role" "authorizer" {
  name               = "raidync-dev-authorizer"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "authorizer_logs" {
  role       = aws_iam_role.authorizer.name
  policy_arn = local.lambda_basic_exec
}

# ---- matchmaking 用ロール（DynamoDB + GameLift + Logs） ----
resource "aws_iam_role" "matchmaking" {
  name               = "raidync-dev-matchmaking"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
  tags               = var.tags
}

resource "aws_iam_role_policy_attachment" "matchmaking_logs" {
  role       = aws_iam_role.matchmaking.name
  policy_arn = local.lambda_basic_exec
}

data "aws_iam_policy_document" "matchmaking" {
  # DynamoDB: コードが実際に使うアクションのみ（Delete は TTL 自動削除に委ねるため不要）。
  # テーブル本体と GSI(/index/*) の両方を対象にする（Query が GSI を使うため）。
  statement {
    sid = "DynamoDBCrud"
    actions = [
      "dynamodb:GetItem",
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
      "dynamodb:TransactWriteItems",
    ]
    resources = concat(
      var.dynamodb_table_arns,
      [for arn in var.dynamodb_table_arns : "${arn}/index/*"],
    )
  }

  # GameLift: セッション作成に必要な 2 アクション。CreateGameSession/CreatePlayerSessions は
  # resource-level 権限のサポートが限定的で、かつ fleet はこの時点で未作成のため Resource="*"。
  statement {
    sid = "GameLiftCreateSessions"
    actions = [
      "gamelift:CreateGameSession",
      "gamelift:CreatePlayerSessions",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "matchmaking" {
  name   = "raidync-dev-matchmaking-inline"
  role   = aws_iam_role.matchmaking.id
  policy = data.aws_iam_policy_document.matchmaking.json
}

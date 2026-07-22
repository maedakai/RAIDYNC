# AWS dev 環境（本番同等構成）。
#
# GameLift は本番と同じ Managed EC2 フリートを使う（ステップ6 で追加予定）。
# 開発検証用の Anywhere フリートは environments/anywhere に分離してある
# （自 PC を compute にする開発専用の手段であり、本番構成の部品ではないため）。

module "dynamodb" {
  source = "../../modules/dynamodb"

  # dev は叩いた分だけの従量課金・PITR 無効でコストを抑える（モジュール既定と同じ）。
  billing_mode           = "PAY_PER_REQUEST"
  point_in_time_recovery = false
  tags                   = var.tags
}

module "iam" {
  source = "../../modules/iam"

  dynamodb_table_arns = module.dynamodb.table_arns
  tags                = var.tags
}

module "cognito" {
  source = "../../modules/cognito"

  name_prefix = "raidync-dev"
  tags        = var.tags
}

module "lambda" {
  source = "../../modules/lambda"

  name_prefix = "raidync-dev"
  # scripts/build-lambdas.sh の出力先。apply 前に必ずビルドしておくこと。
  artifacts_dir = "${path.root}/../../../build/lambda"

  authorizer_role_arn  = module.iam.authorizer_role_arn
  matchmaking_role_arn = module.iam.matchmaking_role_arn

  # authorizer の JWT 検証先を実 Cognito に向ける（mock-auth の置き換え）。
  auth_jwks_url  = module.cognito.jwks_url
  auth_issuer    = module.cognito.issuer
  auth_client_id = module.cognito.client_id

  # GameLift Managed EC2（ステップ6）を作ったら fleet_id を配線する。
  # 空の間は match_status が GameLiftStub にフォールバックする。
  # gamelift_fleet_id = module.gamelift_ec2.fleet_id
  # GAMELIFT_LOCATION は Managed EC2 では空のままにする（Anywhere 用の値のため）。

  tags = var.tags
}

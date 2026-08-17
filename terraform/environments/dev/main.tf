# AWS dev 環境（本番同等構成）。
#
# GameLift は本番と同じ Managed EC2 フリートを使う（ステップ6 で追加予定）。
# 開発検証用の Anywhere フリートは environments/anywhere に分離してある
# （自 PC を compute にする開発専用の手段であり、本番構成の部品ではないため）。

# クロスリージョン共有テーブル（Players / Sessions / MatchHistory）。
# Global Tables なので実体は両リージョンにあるが、Terraform 上はこの 1 モジュールが
# レプリカまで面倒を見る（セカンダリ用 provider は使わない）。
module "dynamodb" {
  source = "../../modules/dynamodb"

  # dev は叩いた分だけの従量課金・PITR 無効でコストを抑える（モジュール既定と同じ）。
  billing_mode           = "PAY_PER_REQUEST"
  point_in_time_recovery = false
  # Phase 5: Players / Sessions / MatchHistory を us-east-1 へレプリケーションする。
  # レプリカ自体に固定費は無く、書き込み（rWCU）と保存量の従量課金のみ。
  replica_regions = var.replica_regions
  tags            = var.tags
}

# region-local な待機チケット表。Global Tables に載せられないため（楽観ロックが
# 結果整合のマルチライターで壊れる）、**各リージョンに独立した実体を 1 つずつ**置く。
# マッチングは自リージョン内で完結し、待機列はリージョンをまたいで統合されない。
module "dynamodb_tickets" {
  source = "../../modules/dynamodb_tickets"

  billing_mode           = "PAY_PER_REQUEST"
  point_in_time_recovery = false
  tags                   = var.tags
}

module "dynamodb_tickets_secondary" {
  source = "../../modules/dynamodb_tickets"
  providers = {
    aws = aws.secondary
  }

  billing_mode           = "PAY_PER_REQUEST"
  point_in_time_recovery = false
  tags                   = var.tags
}

# IAM ロールはグローバルリソースなので 1 インスタンスで両リージョンの Lambda を賄う。
# ただしテーブル ARN はリージョンごとに別物なので、許可対象には
# 東京の 3 テーブル + us-east-1 レプリカ 3 + 両リージョンの Tickets を全て渡す。
module "iam" {
  source = "../../modules/iam"

  dynamodb_table_arns = concat(
    values(module.dynamodb.table_arns),
    module.dynamodb.replica_table_arns,
    [
      module.dynamodb_tickets.table_arn,
      module.dynamodb_tickets_secondary.table_arn,
    ],
  )
  tags = var.tags
}

module "cognito" {
  source = "../../modules/cognito"

  name_prefix = "raidync-dev"
  tags        = var.tags
}

# ⚠ 唯一「立てているだけで時間課金」されるモジュール。E2E 後は必ず destroy すること。
module "gamelift_ec2" {
  source = "../../modules/gamelift_ec2"

  name_prefix = "raidync-dev"
  # scripts/build-gameserver.sh の出力先。apply 前に必ずビルドしておくこと。
  build_dir = "${path.root}/../../../build/gameserver"

  tags = var.tags
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

  # マッチ成立時に実 GameLift へセッションを作らせる。
  # GAMELIFT_LOCATION は Managed EC2 では空のままにする（Anywhere 用の値のため。
  # gamelift_provider.go の locationPtr が空なら Location を省略する）。
  gamelift_fleet_id = module.gamelift_ec2.fleet_id

  tags = var.tags
}

module "api_gateway" {
  source = "../../modules/api_gateway"

  name_prefix = "raidync-dev"
  stage_name  = "dev"

  lambda_invoke_arns    = module.lambda.invoke_arns
  lambda_function_names = module.lambda.function_names

  tags = var.tags
}

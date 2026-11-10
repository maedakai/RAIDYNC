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

# Cognito は**東京の 1 プールを両リージョンで共用する**。
# ユーザープールはリージョンをまたいで複製できないため、リージョンごとに独立させると
# 同じプレイヤーが両方で登録し直す羽目になり破綻する。JWT 検証は JWKS を HTTPS で
# 引いて署名を確かめるだけでリージョン親和性が無いので、us-east-1 の authorizer からも
# 東京のプールのトークンを検証できる。
module "cognito" {
  source = "../../modules/cognito"

  name_prefix = "raidync-dev"
  tags        = var.tags
}

# ⚠ 唯一「立てているだけで時間課金」されるモジュール。
# 既定では作らない（enable_gamelift = false）。E2E 検証時のみ true にし、終わったら
# false に戻して apply すると消える。詳細は variables.tf の enable_gamelift。
module "gamelift_ec2" {
  source = "../../modules/gamelift_ec2"
  count  = var.enable_gamelift ? 1 : 0

  name_prefix = "raidync-dev"
  # scripts/build-gameserver.sh の出力先。apply 前に必ずビルドしておくこと。
  build_dir = "${path.root}/../../../build/gameserver"

  tags = var.tags
}

# セカンダリリージョンのフリート（Phase 5-4）。
# 同じ enable_gamelift で連動させる＝「起動するときは両リージョン、止めるときも両方」。
# 片方だけ残して課金し続ける事故を防ぐため、意図的に別フラグにしていない。
#
# Build は awscc が必要（server_sdk_version が classic aws に無い）なので、
# aws / awscc の両方の provider を alias で渡す。
module "gamelift_ec2_secondary" {
  source = "../../modules/gamelift_ec2"
  count  = var.enable_gamelift ? 1 : 0
  providers = {
    aws   = aws.secondary
    awscc = awscc.secondary
  }

  name_prefix = "raidync-dev"
  build_dir   = "${path.root}/../../../build/gameserver"

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
  # フリート未作成時は空文字を渡す＝match_status が GameLiftStub にフォールバックする
  # （cmd/lambda/match_status/main.go の selectSessionProvider）。
  gamelift_fleet_id = one(module.gamelift_ec2[*].fleet_id)

  # セッションアフィニティの固定先。**リージョン固定名**を渡すこと。
  # レイテンシールーティング対象の api.<domain> を渡すと、DNS が振り直された瞬間に
  # 固定が意味を失う。空なら Lambda がリクエストから execute-api URL を導出する。
  api_base_url = local.dns_enabled ? "https://${local.api_fqdn_primary}" : ""

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

# ---------------------------------------------------------------------------
# セカンダリリージョン（us-east-1）— Phase 5-2
#
# 東京と同じ Lambda 6 関数 + REST API をもう一組立てる。プライマリとの違いは
# provider だけで、モジュールもビルド成果物も同一のものを使い回す。
#
# ここで DynamoDB のリージョン局所性が自動的に成立する: Lambda ランタイムが
# AWS_REGION=us-east-1 を自動設定し、internal/db.NewClient がそれを読んで
# SDK のリージョンにするため、Go 側の変更は一切要らない。
#
#   Global Tables の 3 表 … us-east-1 のレプリカを読み書き（東京と同一データ）
#   MatchmakingTickets   … us-east-1 の独立テーブル（東京とは別データ）
#
# 認証は東京の Cognito を共用する（プールは複製できないため）。
# GameLift は判断どおり検証時のみなので、セカンダリのフリートはまだ作らない
# （fleet_id 未指定 → match_status は GameLiftStub にフォールバック）。
# ---------------------------------------------------------------------------

module "lambda_secondary" {
  source = "../../modules/lambda"
  providers = {
    aws = aws.secondary
  }

  name_prefix   = "raidync-dev"
  artifacts_dir = "${path.root}/../../../build/lambda"

  # IAM ロールはグローバルなので東京と同じものを使う。
  authorizer_role_arn  = module.iam.authorizer_role_arn
  matchmaking_role_arn = module.iam.matchmaking_role_arn

  # 東京の Cognito を共用（JWT 検証はリージョン非依存）。
  auth_jwks_url  = module.cognito.jwks_url
  auth_issuer    = module.cognito.issuer
  auth_client_id = module.cognito.client_id

  # セッションアフィニティの固定先（このリージョン固有名）。
  api_base_url = local.dns_enabled ? "https://${local.api_fqdn_secondary}" : ""

  # このリージョンのフリートに繋ぐ。us-east-1 でマッチしたプレイヤーが
  # 東京のゲームサーバーに飛ばされては本末転倒なので、必ず自リージョンのものを渡す。
  gamelift_fleet_id = one(module.gamelift_ec2_secondary[*].fleet_id)

  tags = var.tags
}

module "api_gateway_secondary" {
  source = "../../modules/api_gateway"
  providers = {
    aws = aws.secondary
  }

  name_prefix = "raidync-dev"
  stage_name  = "dev"

  lambda_invoke_arns    = module.lambda_secondary.invoke_arns
  lambda_function_names = module.lambda_secondary.function_names

  tags = var.tags
}


# monitoring
module "monitoring" {
  source               = "../../modules/monitoring"
  alarm_function_names = module.lambda.function_names
  notification_email   = var.notification_email
  sampling_fixed_rate  = var.sampling_fixed_rate
  tags                 = var.tags
}

module "monitoring_secondary" {
  source = "../../modules/monitoring"
  providers = {
    aws   = aws.secondary
    awscc = awscc.secondary
  }
  alarm_function_names = module.lambda_secondary.function_names
  notification_email   = var.notification_email
  sampling_fixed_rate  = var.sampling_fixed_rate
  tags                 = var.tags
}

# monitoring_dashboard
module "monitoring_dashboard" {
  source                   = "../../modules/monitoring_dashboard"
  dashboard_name           = "raidync-dev-loadtest"
  function_names_primary   = module.lambda.function_names
  function_names_secondary = module.lambda_secondary.function_names
  primary_region           = var.region
  secondary_region         = var.secondary_region

  table_names = concat(
    values(module.dynamodb.table_names),
    [module.dynamodb_tickets.table_name]
  )

  rest_api_name_primary   = module.api_gateway.rest_api_name
  rest_api_name_secondary = module.api_gateway_secondary.rest_api_name
  stage_name              = "dev"
}

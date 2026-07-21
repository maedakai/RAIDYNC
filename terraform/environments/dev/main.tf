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

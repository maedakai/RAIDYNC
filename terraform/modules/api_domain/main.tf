# API Gateway のリージョナルカスタムドメインと、REST API ステージへの紐付け。
#
# 1 リージョンにつき 2 つの名前を載せる:
#   api.<domain>          … Route 53 のレイテンシールーティング対象（クライアントの入口）。
#                           **同じ名前を両リージョンに作る**。これが AWS 公式の
#                           マルチリージョン API パターンで、DNS 側で振り分ける。
#   <prefix>.api.<domain> … リージョン固定名。セッションアフィニティ用に
#                           match/request が API_BASE_URL として返す先。
#                           レイテンシールーティングされないため、途中で別リージョンへ
#                           飛ばされることがない。
#
# 課金: カスタムドメイン自体に追加料金は無い（証明書も AWS サービス用途なら無料）。

resource "aws_api_gateway_domain_name" "this" {
  for_each = toset(var.domain_names)

  domain_name              = each.value
  regional_certificate_arn = var.certificate_arn

  endpoint_configuration {
    types = ["REGIONAL"]
  }

  # 既定より新しい TLS を要求する（古い 1.0 を許さない）。
  security_policy = "TLS_1_2"

  tags = var.tags
}

# カスタムドメインのルートパスを REST API のステージに割り当てる。
# base_path を指定しないため https://<domain>/match/request のように
# ステージ名（/dev）がパスに出ない。execute-api の URL とはこの点が異なる。
resource "aws_api_gateway_base_path_mapping" "this" {
  for_each = aws_api_gateway_domain_name.this

  api_id      = var.rest_api_id
  stage_name  = var.stage_name
  domain_name = each.value.domain_name
}

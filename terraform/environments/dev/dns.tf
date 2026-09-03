# カスタムドメイン・証明書・Route 53 レイテンシールーティング（Phase 5-3b）。
#
# domain_name が空なら丸ごと作らない（ドメイン未取得でも apply できる）。
#
# ここに置く名前:
#   api.<domain>          … レイテンシールーティング対象。両リージョンに**同名**で作る。
#                           クライアントの入口で、近い方のリージョンへ自動で振られる
#   apne1.api.<domain>    … 東京固定（セッションアフィニティ用）
#   use1.api.<domain>     … バージニア固定（同上）
#
# 処理の順序が肝で、次の 4 段階を経る:
#   1) 証明書を要求（リージョンごと。api_cert モジュール）
#   2) 検証用 CNAME を Route 53 に作り、検証完了を待つ（このファイル。重複排除が要る）
#   3) 検証済み証明書でカスタムドメインを作る（api_domain モジュール）
#   4) Route 53 レコードを張る（固定名 → レイテンシー。ヘルスチェックは opt-in）
# 2 を挟むためにモジュールを 2 つに割っている。

locals {
  dns_enabled = var.domain_name != ""

  # レイテンシールーティングで使う共通名。
  api_fqdn = local.dns_enabled ? "api.${var.domain_name}" : ""

  # リージョン固定名。API_BASE_URL に渡してセッションアフィニティを効かせる。
  # 共通名（api.<domain>）を渡してはいけない（DNS が振り直された瞬間に固定が壊れる）。
  api_fqdn_primary   = local.dns_enabled ? "apne1.${local.api_fqdn}" : ""
  api_fqdn_secondary = local.dns_enabled ? "use1.${local.api_fqdn}" : ""

  primary_domain_names   = local.dns_enabled ? [local.api_fqdn, local.api_fqdn_primary] : []
  secondary_domain_names = local.dns_enabled ? [local.api_fqdn, local.api_fqdn_secondary] : []
}

# ドメイン登録時に自動作成されたホストゾーンを引く。
# 登録自体は Terraform で管理しない（商取引であり、aws_route53domains_registered_domain は
# 登録済みドメインの設定管理しかできない）。docs/qa-log.md §13 参照。
data "aws_route53_zone" "this" {
  count = local.dns_enabled ? 1 : 0

  name         = "${var.domain_name}."
  private_zone = false
}

# ---- 1) 証明書の要求（リージョンごと） ----

module "api_cert" {
  source = "../../modules/api_cert"
  count  = local.dns_enabled ? 1 : 0

  domain_names = local.primary_domain_names
  tags         = var.tags
}

module "api_cert_secondary" {
  source = "../../modules/api_cert"
  count  = local.dns_enabled ? 1 : 0
  providers = {
    aws = aws.secondary
  }

  domain_names = local.secondary_domain_names
  tags         = var.tags
}

# ---- 2) DNS 検証 ----

# 両リージョンの証明書が同じ api.<domain> を含むため、ACM が返す検証レコードは
# その名前について同名・同値になる（ACM は同一アカウント・同一ドメインなら同じ
# 検証レコードを返す＝1 本作れば両方の証明書が検証される）。
# 重複を排除しないと同じ Route 53 レコードを 2 リソースが取り合い、destroy 時に
# 「片方が消した後にもう片方が消そうとして失敗する」事故になる。
locals {
  # for_each のキーは plan 時に確定していなければならない。ACM の
  # resource_record_name は apply 後にしか決まらないので、**設定から静的に分かる
  # ドメイン名**を集合にしてキーにする。toset がそのまま重複排除も兼ねる。
  cert_validation_domains = local.dns_enabled ? toset(concat(
    local.primary_domain_names,
    local.secondary_domain_names,
  )) : toset([])

  # ドメイン名 => 検証レコード情報。同名（api.<domain>）が両リージョンから来るため
  # グルーピング（... 記法）で受けて、先頭を採る。値は同一なのでどちらでもよい。
  cert_validation_by_domain = local.dns_enabled ? {
    for o in concat(
      module.api_cert[0].domain_validation_options,
      module.api_cert_secondary[0].domain_validation_options,
    ) : o.domain_name => o...
  } : {}
}

resource "aws_route53_record" "cert_validation" {
  for_each = local.cert_validation_domains

  zone_id = data.aws_route53_zone.this[0].zone_id
  name    = local.cert_validation_by_domain[each.key][0].resource_record_name
  type    = local.cert_validation_by_domain[each.key][0].resource_record_type
  records = [local.cert_validation_by_domain[each.key][0].resource_record_value]
  ttl     = 60

  # 同名の検証レコードが既にあっても上書きして進む（証明書の再作成時に詰まらせない）。
  allow_overwrite = true
}

# 検証完了（証明書が ISSUED になる）まで待つ。リージョンごとに必要。
resource "aws_acm_certificate_validation" "primary" {
  count = local.dns_enabled ? 1 : 0

  certificate_arn         = module.api_cert[0].certificate_arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

resource "aws_acm_certificate_validation" "secondary" {
  count    = local.dns_enabled ? 1 : 0
  provider = aws.secondary

  certificate_arn         = module.api_cert_secondary[0].certificate_arn
  validation_record_fqdns = [for r in aws_route53_record.cert_validation : r.fqdn]
}

# ---- 3) カスタムドメイン ----

module "api_domain" {
  source = "../../modules/api_domain"
  count  = local.dns_enabled ? 1 : 0

  domain_names = local.primary_domain_names
  # 検証済み ARN を渡す（証明書リソースの arn を直接渡すと ISSUED 前に走って失敗し得る）。
  certificate_arn = aws_acm_certificate_validation.primary[0].certificate_arn
  rest_api_id     = module.api_gateway.rest_api_id
  stage_name      = "dev"

  tags = var.tags
}

module "api_domain_secondary" {
  source = "../../modules/api_domain"
  count  = local.dns_enabled ? 1 : 0
  providers = {
    aws = aws.secondary
  }

  domain_names    = local.secondary_domain_names
  certificate_arn = aws_acm_certificate_validation.secondary[0].certificate_arn
  rest_api_id     = module.api_gateway_secondary.rest_api_id
  stage_name      = "dev"

  tags = var.tags
}

# ---- 4) Route 53 レコード（Phase 5-3b-2） ----

# リージョン固定名。単純な alias で、それぞれ自分のリージョンだけを指す。
# セッションアフィニティ（match/request が返す API_BASE_URL）の実体はこれ。
# レイテンシールーティングしないので、DNS が途中で別リージョンへ振り直すことがない。
resource "aws_route53_record" "api_regional" {
  for_each = local.dns_enabled ? {
    (local.api_fqdn_primary)   = module.api_domain[0].regional_endpoints[local.api_fqdn_primary]
    (local.api_fqdn_secondary) = module.api_domain_secondary[0].regional_endpoints[local.api_fqdn_secondary]
  } : {}

  zone_id = data.aws_route53_zone.this[0].zone_id
  name    = each.key
  type    = "A"

  alias {
    name    = each.value.regional_domain_name
    zone_id = each.value.regional_zone_id
    # API Gateway は alias のターゲットヘルスを報告しないため false 固定。
    # 生死判定は下の aws_route53_health_check で明示的に行う。
    evaluate_target_health = false
  }
}

# ヘルスチェック（enable_health_checks で opt-in）。
#
# 監視先に**リージョン固定名**を使うのが肝。共通名 api.<domain> を指すと、
# 世界中に散らばる Route 53 のヘルスチェッカーがそれぞれ自分の最寄りへ解決してしまい、
# 「特定リージョンの生死」を測れなくなる。
resource "aws_route53_health_check" "api" {
  for_each = local.dns_enabled && var.enable_health_checks ? {
    (var.region)           = local.api_fqdn_primary
    (var.secondary_region) = local.api_fqdn_secondary
  } : {}

  fqdn          = each.value
  port          = 443
  type          = "HTTPS"
  resource_path = "/health" # MOCK 統合で 200 を返す（Lambda を呼ばない）

  # spec-architecture.md §2.7 の「30 秒間隔」。3 回連続失敗で異常と判定する。
  request_interval  = 30
  failure_threshold = 3

  tags = merge(var.tags, { Name = "raidync-dev-health-${each.key}" })

  # 監視先の名前が解決できるようになってから作る。
  depends_on = [aws_route53_record.api_regional]
}

# レイテンシールーティング。**同名のレコードを 2 本**置き、set_identifier で区別する。
# Route 53 は問い合わせ元から見て最も低レイテンシーなリージョンの方を返す。
# ヘルスチェックを付けた場合、異常なリージョンは候補から外れる（＝フェイルオーバー）。
resource "aws_route53_record" "api_latency" {
  for_each = local.dns_enabled ? {
    (var.region)           = module.api_domain[0].regional_endpoints[local.api_fqdn]
    (var.secondary_region) = module.api_domain_secondary[0].regional_endpoints[local.api_fqdn]
  } : {}

  zone_id        = data.aws_route53_zone.this[0].zone_id
  name           = local.api_fqdn
  type           = "A"
  set_identifier = each.key

  latency_routing_policy {
    region = each.key
  }

  alias {
    name                   = each.value.regional_domain_name
    zone_id                = each.value.regional_zone_id
    evaluate_target_health = false
  }

  # 未作成なら null＝ヘルスチェック無しのレイテンシールーティングになる。
  health_check_id = try(aws_route53_health_check.api[each.key].id, null)
}

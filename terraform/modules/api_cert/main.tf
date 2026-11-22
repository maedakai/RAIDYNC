# API Gateway リージョナルカスタムドメイン用の ACM 証明書。
#
# なぜリージョンごとに要るか:
#   REGIONAL エンドポイントのカスタムドメインは、**同一リージョンにある証明書**しか
#   使えない。CloudFront（EDGE）のように us-east-1 の証明書を使い回すことはできないため、
#   東京とバージニアでそれぞれ発行する。証明書自体は AWS サービスで使う限り無料。
#
# 証明書の作成と「検証（validation）」を分けている理由:
#   DNS 検証は Route 53 に CNAME を置いて所有権を示す方式。そのレコードはホストゾーン
#   （グローバル）に作るので、リージョンごとのモジュール内では完結しない。さらに両リージョンが
#   同じ api.<domain> を要求するため、ACM が返す検証レコードは**同名・同値になり衝突する**
#   （ACM は同一アカウント・同一ドメインなら同じ検証レコードを返す＝1 本で両方を検証できる）。
#   したがってレコード作成と aws_acm_certificate_validation は呼び出し側（env root）で
#   重複排除してから行う。ここでは証明書の要求と、検証に必要な情報の公開だけを担う。

resource "aws_acm_certificate" "this" {
  # 1 枚の証明書で複数名を賄う（レイテンシールーティング用の共通名 + リージョン固定名）。
  domain_name               = var.domain_names[0]
  subject_alternative_names = slice(var.domain_names, 1, length(var.domain_names))
  validation_method         = "DNS"

  # 名前を変更するときは新証明書を先に作ってから差し替える（ダウンタイムを避ける）。
  lifecycle {
    create_before_destroy = true
  }

  tags = var.tags
}

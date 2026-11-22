
resource "aws_cloudwatch_metric_alarm" "this" {
  for_each            = var.alarm_function_names
  alarm_name          = "${each.value}-errors"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = 1
  metric_name         = "HandlerError"
  namespace           = "RAIDYNC/Handlers"
  period              = var.period
  statistic           = "Sum"
  threshold           = var.threshold

  dimensions = {
    FunctionName = each.value
  }

  alarm_actions      = [aws_sns_topic.this.arn]
  treat_missing_data = "notBreaching"
  # この文面は通知メールの本文に載る。メールだけで一次切り分けができるよう、
  # 「何が起きたか」「どこを見るか」「次の一手」を入れる（上限 1024 文字）。
  alarm_description = <<-EOT
    ${each.value} がサーバー側エラー（5xx）を返した。${var.period} 秒間に ${var.threshold} 件以上。

    1. ログ: /aws/lambda/${each.value} の level=ERROR 行。err に原因の原文が入っている。
    2. 内訳: メトリクス RAIDYNC/Handlers の HandlerError を
       ディメンション FunctionName + ErrorKind で見ると、どの種類のエラーかが分かる。

    クライアント起因（4xx）はこのアラームに含まれない。鳴った時点でサーバー側の異常。
  EOT

  tags = var.tags
}

resource "aws_sns_topic" "this" {
  name = "raidync-dev-handler-errors"
  tags = var.tags
}

resource "aws_sns_topic_subscription" "this" {
  count = var.notification_email != "" ? 1 : 0

  topic_arn = aws_sns_topic.this.arn
  protocol  = "email"
  endpoint  = var.notification_email
}

# ---- Transaction Search（X-Ray の OTLP エンドポイントの前提） ----
# Lambda は Span を X-Ray の OTLP エンドポイントへ直接送る（internal/telemetry）。この経路は
# Transaction Search が無効だと 400 で拒否される。有効にすると Span は CloudWatch Logs の
# aws/spans に保存される。設定はリージョン単位なので、このモジュール（リージョンごとに呼ばれる）に置く。
# region / account は呼び出し側の provider から読む（secondary なら us-east-1 が返る）。
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  region  = data.aws_region.current.name
  account = data.aws_caller_identity.current.account_id
}

# X-Ray が aws/spans（と Application Signals 用のロググループ）へ書き込むことを許すリソースポリシー。
# 条件で「このアカウント・このリージョンの X-Ray から」に絞る（confused deputy 対策）。
data "aws_iam_policy_document" "xray" {
  statement {
    sid     = "XRayWrite"
    actions = ["logs:PutLogEvents"]
    resources = [
      "arn:aws:logs:${local.region}:${local.account}:log-group:aws/spans:*",
      "arn:aws:logs:${local.region}:${local.account}:log-group:/aws/application-signals/data:*",
    ]
    principals {
      type        = "Service"
      identifiers = ["xray.amazonaws.com"]
    }
    condition {
      test     = "ArnLike"
      variable = "aws:SourceArn"
      values = [
        "arn:aws:xray:${local.region}:${local.account}:*",
      ]
    }
    condition {
      test     = "StringEquals"
      variable = "aws:SourceAccount"
      values   = [local.account]
    }
  }
}

resource "aws_cloudwatch_log_resource_policy" "xray" {
  policy_name     = "raidync-dev-xray-transaction-search"
  policy_document = data.aws_iam_policy_document.xray.json
}

# 送り先を CloudWatch Logs に切り替え、Span の 1% を索引付けする（1% までは無料）。
# Span 自体は 100% aws/spans に入り検索できる。索引はトレース一覧・分析用の要約だけに効く。
# 参照関係が無いため depends_on で順序を明示する（リソースポリシーが先に無いと X-Ray が書き込めない）。
# classic の aws provider には該当リソースが無く、awscc（AWS::XRay::TransactionSearchConfig）を使う。
resource "awscc_xray_transaction_search_config" "this" {
  depends_on          = [aws_cloudwatch_log_resource_policy.xray]
  indexing_percentage = 1
}

# どのリクエストを記録するか（サンプリング）を決めるルール。API Gateway が入口で判定し、結果は
# 下流の Lambda にも引き継がれる（記録しないと決まったリクエストは Lambda の Span も送られない）。
# 条件はすべて "*" で全リクエストに当てる。priority 9999 は既定ルール（10000）より優先されるため、
# 割合は常にこのルールの fixed_rate で決まる。普段は既定ルールと同じ値（毎秒 1 件 + 5%）にしておき、
# 遅いリクエストを全件追いたいときだけ変数で 1.0 に上げる（取り込み課金が増えるため検証後は戻す）。
resource "aws_xray_sampling_rule" "this" {
  rule_name      = "raidync-dev-sampling-rule"
  priority       = 9999
  fixed_rate     = var.sampling_fixed_rate
  reservoir_size = 1
  service_name   = "*"
  service_type   = "*"
  resource_arn   = "*"
  host           = "*"
  http_method    = "*"
  url_path       = "*"
  version        = 1
}

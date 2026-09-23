
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

output "certificate_arn" {
  description = "発行された証明書の ARN。検証完了前は使えないので、呼び出し側は aws_acm_certificate_validation を経由すること。"
  value       = aws_acm_certificate.this.arn
}

output "domain_validation_options" {
  description = <<-EOT
    DNS 検証に必要なレコード情報（name / type / value）のリスト。
    呼び出し側でリージョン間の重複を排除してから Route 53 に作る。
  EOT
  value = [
    for o in aws_acm_certificate.this.domain_validation_options : {
      resource_record_name  = o.resource_record_name
      resource_record_type  = o.resource_record_type
      resource_record_value = o.resource_record_value
    }
  ]
}

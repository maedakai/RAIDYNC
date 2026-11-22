output "regional_endpoints" {
  description = <<-EOT
    ドメイン名 => { regional_domain_name, regional_zone_id }。
    Route 53 の alias レコードの向き先に使う（A レコードの alias は
    この 2 つを指定する。IP を直接書くわけではない）。
  EOT
  value = {
    for name, d in aws_api_gateway_domain_name.this : name => {
      regional_domain_name = d.regional_domain_name
      regional_zone_id     = d.regional_zone_id
    }
  }
}

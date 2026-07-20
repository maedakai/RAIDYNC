output "location_name" {
  description = "カスタムロケーション名。"
  value       = module.gamelift_anywhere.location_name
}

output "fleet_id" {
  description = "Anywhere フリート ID。"
  value       = module.gamelift_anywhere.fleet_id
}

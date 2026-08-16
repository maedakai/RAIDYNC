output "location_name" {
  description = "カスタムロケーション名。register-compute の --location に渡す。"
  value       = module.gamelift_anywhere.location_name
}

output "fleet_id" {
  description = "Anywhere フリート ID。register-compute / create-game-session の --fleet-id に渡す。"
  value       = module.gamelift_anywhere.fleet_id
}

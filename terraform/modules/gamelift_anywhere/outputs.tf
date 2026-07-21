output "location_name" {
  description = "作成したカスタムロケーション名。"
  value       = awscc_gamelift_location.this.location_name
}

output "fleet_id" {
  description = "Anywhere フリート ID（register-compute / create-game-session で使用）。"
  value       = awscc_gamelift_fleet.this.fleet_id
}

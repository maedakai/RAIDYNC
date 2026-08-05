output "fleet_id" {
  description = "Managed EC2 フリート ID。match_status Lambda の GAMELIFT_FLEET_ID に渡す。"
  value       = aws_gamelift_fleet.this.id
}

output "fleet_arn" {
  description = "フリート ARN。"
  value       = aws_gamelift_fleet.this.arn
}

output "build_id" {
  description = "GameLift Build ID。"
  value       = awscc_gamelift_build.this.build_id
}

output "build_bucket" {
  description = "Build zip を置いた S3 バケット名。"
  value       = aws_s3_bucket.build.id
}

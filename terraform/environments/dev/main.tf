module "gamelift_anywhere" {
  source = "../../modules/gamelift"

  location_name = var.location_name
  fleet_name    = var.fleet_name
  tags          = var.tags
}

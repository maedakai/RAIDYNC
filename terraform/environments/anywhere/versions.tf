terraform {
  required_version = ">= 1.5"

  required_providers {
    # Anywhere（custom location / ANYWHERE fleet）は classic aws provider が表現できないため
    # Cloud Control 系の awscc を使う。詳細は modules/gamelift_anywhere/main.tf 参照。
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.0"
    }
  }
}

provider "awscc" {
  region = var.region
}

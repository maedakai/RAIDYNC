terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    # gamelift_ec2 の Build のみ awscc を使う（server_sdk_version が classic aws に無いため）。
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

provider "aws" {
  region = var.region
}

provider "awscc" {
  region = var.region
}

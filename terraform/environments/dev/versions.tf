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

# Phase 5: セカンダリリージョン（us-east-1）。region-local な MatchmakingTickets と、
# セカンダリの Lambda / API Gateway をここに作る。
# Global Tables のレプリカはこの alias を使わない（replica ブロックが AWS 側に作らせる）。
provider "aws" {
  alias  = "secondary"
  region = var.secondary_region
}

provider "awscc" {
  region = var.region
}

# gamelift_ec2 をセカンダリにも建てる場合に使う（Build は awscc が必要）。
provider "awscc" {
  alias  = "secondary"
  region = var.secondary_region
}

terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    # ビルド済み bootstrap を zip 化するために使う（外部ビルド → archive_file 方式）。
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

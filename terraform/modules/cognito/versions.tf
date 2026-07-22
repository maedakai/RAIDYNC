terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    # E2E テストユーザーのパスワードを自動生成する（値をリポジトリに置かないため）。
    random = {
      source  = "hashicorp/random"
      version = "~> 3.0"
    }
  }
}

terraform {
  required_version = ">= 1.5"

  required_providers {
    # Managed EC2 フリートは classic aws provider で表現できる
    # （aws_gamelift_fleet は ec2_instance_type 必須＝EC2 専用リソース）。
    # Anywhere だけが awscc を要したのであり、こちらは aws provider でよい。
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

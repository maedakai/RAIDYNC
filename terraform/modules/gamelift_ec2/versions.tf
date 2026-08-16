terraform {
  required_version = ">= 1.5"

  required_providers {
    # Fleet は classic aws provider（aws_gamelift_fleet は ec2_instance_type 必須＝EC2 専用）。
    # ただし Build は awscc が必要:classic の aws_gamelift_build は server_sdk_version を
    # 公開しておらず、AL2023 × SDK 5.x（本プロジェクトの Go サーバー SDK 5.3.0）の Build を
    # 作れない。よって Build のみ awscc_gamelift_build を使い、Fleet は build_id で参照する。
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
  }
}

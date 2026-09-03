terraform {
  required_version = ">= 1.5"

  required_providers {
    # DynamoDB は classic aws provider を使う（aws_dynamodb_table は GSI/TTL/PITR を
    # 素直に表現でき枯れている）。GameLift Anywhere のみ awscc が必要な特殊事情だった。
    # 5.x に固定。6.x は hash_key/range_key を deprecated 化しており、正準な
    # top-level キー記法で警告が出るため、安定メジャーの 5 系を使う。
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

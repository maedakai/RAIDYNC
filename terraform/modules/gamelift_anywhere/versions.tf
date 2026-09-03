terraform {
  required_version = ">= 1.5"

  required_providers {
    # classic aws provider は GameLift Anywhere（custom location / ANYWHERE fleet）を
    # 表現できないため、CloudFormation 定義から自動生成される Cloud Control 系の
    # awscc provider を使う。詳細は modules/gamelift/main.tf のコメント参照。
    awscc = {
      source  = "hashicorp/awscc"
      version = "~> 1.0"
    }
  }
}

terraform {
  required_version = ">= 1.5"

  required_providers {
    # modules/dynamodb と同じ理由で 5.x に固定（6.x は hash_key/range_key を deprecated 化）。
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

variable "region" {
  description = "デプロイ先リージョン。"
  type        = string
  default     = "ap-northeast-1"
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default = {
    Project = "RAIDYNC"
    Env     = "dev"
    Phase   = "4"
  }
}

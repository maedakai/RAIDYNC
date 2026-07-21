variable "region" {
  description = "デプロイ先リージョン。"
  type        = string
  default     = "ap-northeast-1"
}

variable "location_name" {
  description = "GameLift Anywhere カスタムロケーション名。"
  type        = string
  default     = "custom-raidync-local"
}

variable "fleet_name" {
  description = "GameLift Anywhere フリート名。"
  type        = string
  default     = "raidync-anywhere"
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default = {
    Project = "RAIDYNC"
    Env     = "anywhere"
    Phase   = "4"
  }
}

variable "location_name" {
  description = "GameLift Anywhere カスタムロケーション名。^custom- で始まり 8〜64 文字（AWS 制約）。"
  type        = string

  validation {
    condition = (
      can(regex("^custom-[A-Za-z0-9-]+$", var.location_name)) &&
      length(var.location_name) >= 8 &&
      length(var.location_name) <= 64
    )
    error_message = "location_name は ^custom-[A-Za-z0-9-]+ に一致し 8〜64 文字である必要があります。"
  }
}

variable "fleet_name" {
  description = "GameLift Anywhere フリート名。"
  type        = string
}

variable "description" {
  description = "フリートの説明。"
  type        = string
  default     = "RAIDYNC GameLift Anywhere fleet (Phase 4 G4 で手動疎通した構成を IaC 化)"
}

variable "tags" {
  description = "リソースに付与するタグ（key => value）。"
  type        = map(string)
  default     = {}
}

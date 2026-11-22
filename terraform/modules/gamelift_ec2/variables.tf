variable "name_prefix" {
  description = "リソース名の接頭辞。"
  type        = string
  default     = "raidync-dev"
}

variable "build_dir" {
  description = <<-EOT
    scripts/build-gameserver.sh が出力した Build 一式のディレクトリ。
    gameserver / install.sh / start.sh を含む必要がある（apply 前にビルドすること）。
  EOT
  type        = string
}

variable "ec2_instance_type" {
  description = "フリートのインスタンスタイプ。spec-architecture.md §2.2 の c5.large。"
  type        = string
  default     = "c5.large"
}

variable "fleet_type" {
  description = <<-EOT
    SPOT か ON_DEMAND。CLAUDE.md の方針どおり Spot をメインにする（約 70% 安い）。
    On-Demand はフォールバック用途。
  EOT
  type        = string
  default     = "SPOT"

  validation {
    condition     = contains(["SPOT", "ON_DEMAND"], var.fleet_type)
    error_message = "fleet_type は SPOT か ON_DEMAND のいずれか。"
  }
}

# 注: フリートの capacity（desired/min/max インスタンス数）は aws_gamelift_fleet では
# 管理できない（Terraform リソースに該当引数が無く、UpdateFleetCapacity API の領分）。
# 作成時は GameLift 既定の desired=1 になる。dev E2E はこれで足りるため追従しない。
# オートスケール（spec-architecture.md §2.2 の min1/max20）は Phase 6 の担当。

variable "game_port" {
  description = "ゲームサーバが listen する UDP ポート。start.sh の GAMESERVER_BIND と一致させること。"
  type        = number
  default     = 7777
}

variable "allowed_ip_range" {
  description = <<-EOT
    ゲームポートへの接続を許可する CIDR。既定は全開放（クライアントの接続元が
    不定なゲームサーバのため）。絞れるなら絞ること。
  EOT
  type        = string
  default     = "0.0.0.0/0"
}

variable "operating_system" {
  description = "Build の OS。Go 静的バイナリなので Amazon Linux 2023 を使う。"
  type        = string
  default     = "AMAZON_LINUX_2023"
}

variable "server_sdk_version" {
  description = "ゲームサーバーが使う GameLift Server SDK のバージョン。AL2023 は 5.0 以上が必須。cmd/gameserver が依存する Go 版 SDK（amazon-gamelift-servers-go-server-sdk）の SdkVersion 定数に合わせる。"
  type        = string
  default     = "5.3.0"
}

variable "session_api_url" {
  description = "ゲームサーバーがセッション終了時に /session/end で結果を報告する API のベース URL（例: https://<id>.execute-api.<region>.amazonaws.com/dev）。フリートと同じリージョンの API を渡す。"
  type        = string
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}

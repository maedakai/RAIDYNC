# GameLift Anywhere 検証環境。
#
# 位置づけ: 「環境=ローカル寄り × フリート種別=Anywhere」（docs/qa-log.md §11 の2軸整理）。
# 自 PC を compute として登録し、GameLift Server SDK 契約（InitSDK→ProcessReady→
# OnStartGameSession→ProcessEnding）を課金前に検証するための**開発用**環境であり、
# 本番構成の一部ではない。本番同等の構成は environments/dev（Managed EC2 フリート）が担う。
#
# そのため dev とは state を分離する。dev の apply/destroy サイクルに、
# 本来無関係な検証用フリートを巻き込まないことが分離の主目的。
#
# 手順は docs/phase4-anywhere-runbook.md。register-compute / get-compute-auth-token は
# 短命・動的なため IaC 対象外（CLI 実行のまま）。

module "gamelift_anywhere" {
  source = "../../modules/gamelift_anywhere"

  location_name = var.location_name
  fleet_name    = var.fleet_name
  tags          = var.tags
}

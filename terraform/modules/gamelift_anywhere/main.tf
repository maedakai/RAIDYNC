# GameLift Anywhere（custom location + ANYWHERE fleet）を IaC 化するモジュール。
#
# なぜ awscc(Cloud Control) provider か:
#   - classic aws provider には gamelift_location リソースが存在しない。
#   - aws_gamelift_fleet は ec2_instance_type 必須で compute_type=ANYWHERE を扱えない。
#   よって CloudFormation(AWS::GameLift::Location / AWS::GameLift::Fleet) から自動生成される
#   awscc_gamelift_location / awscc_gamelift_fleet を使う。
#
# 対象範囲: Phase 4 G4 で手動疎通した location/fleet の再現のみ。
#   register-compute / get-compute-auth-token は短命・動的なので IaC 対象外（CLI 実行時のまま）。

# カスタムロケーション（Anywhere フリートの紐付け先）
resource "awscc_gamelift_location" "this" {
  location_name = var.location_name

  # awscc のタグは [{ key, value }] 形式。
  tags = [for k, v in var.tags : {
    key   = k
    value = v
  }]
}

# Anywhere フリート
resource "awscc_gamelift_fleet" "this" {
  name         = var.fleet_name
  description  = var.description
  compute_type = "ANYWHERE"

  # Anywhere フリートは EC2 を持たない。cost は per-unit の申告値（自前ホストのため 0）。
  anywhere_configuration = {
    cost = "0"
  }

  # 上で作成したカスタムロケーションに紐付ける（参照で暗黙依存）。
  locations = [{
    location = awscc_gamelift_location.this.location_name
  }]

  # awscc_gamelift_fleet には差分ゼロでも置換を強制する既知バグ(issue #1428)がある。
  # 実 apply は AWS dev E2E 直前に行う予定。そこで不要な置換が出たら該当属性をここで無視する。
  lifecycle {
    ignore_changes = [
      anywhere_configuration,
    ]
  }
}

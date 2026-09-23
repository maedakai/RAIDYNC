variable "alarm_function_names" {
  description = <<-EOT
    アラームを張る対象。**関数キー => Lambda 関数名**の map。
    通常は module.lambda の function_names output をそのまま渡す
    （そうすると関数を追加したときアラームも自動で増え、足し忘れが起きない）。

    **値は EMF が出力する FunctionName ディメンションと完全一致させること**。
    EMF は環境変数 AWS_LAMBDA_FUNCTION_NAME をそのまま使うため、
    接頭辞込みの実際の関数名（例 raidync-dev-match_request）になる。
    短縮キー（match_request）を渡すと存在しないメトリクスを監視することになり、
    アラームは INSUFFICIENT_DATA のまま永久に発火しない。
  EOT
  type        = map(string)
}

variable "period" {
  description = <<-EOT
    アラームの評価期間（**秒**）。既定 300（5 分）＝ エラー発生から通知までの最大遅延。
    長くすると検知が遅れ、短くすると単発のエラーでも鳴りやすくなる。
  EOT
  type        = number
  default     = 300
}

variable "threshold" {
  description = <<-EOT
    アラームを発火させる**エラー件数**。period 内の Sum と比較する
    （既定 1 + period 300 = 「5 分間に 1 件でも 5xx が出たら鳴る」）。

    既定を 1 にしているのは、正常時の HandlerError が 0 件だから。
    運用してノイズが多いようなら上げる。しきい値の変更はメトリクスに影響しない。
  EOT
  type        = number
  default     = 1
}

variable "notification_email" {
  description = <<-EOT
    アラーム通知先のメールアドレス。**空（既定）なら購読を作らない**ので、
    メールを設定していなくても apply は通る（トピックとアラームだけが作られる）。

    値は tfvars ではなく環境変数 `TF_VAR_notification_email` で渡す。
    このリポジトリは Phase 7 で公開予定で、dev.tfvars は Git 管理下にあるため、
    個人のアドレスをコミットしない運用にしている（*.tfvars.local は .gitignore 済み）。

    購読は**確認メールのリンクを人間がクリックするまで有効にならない**。
    apply が成功しても、未確認の間はアラームが鳴っても通知は届かない。
    しかも未達のメッセージは保存されないので、後から確認しても遡って届かない。
  EOT
  type        = string
  default     = ""
  sensitive   = true
}

variable "tags" {
  description = "共通タグ。"
  type        = map(string)
  default     = {}
}
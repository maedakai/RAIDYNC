variable "dashboard_name" {
  description = "CloudWatch ダッシュボード名。"
  type        = string
  default     = "raidync-dev-loadtest"
}

variable "function_names_primary" {
  description = <<-EOT
    プライマリ（東京）の**関数キー => Lambda 関数名**の map。module.lambda の
    function_names output を渡す。値は EMF が出す FunctionName ディメンションと
    一致させること（接頭辞込みの実名）。既定値を置かないのは、渡し忘れたときに
    間違ったダッシュボードが静かに作られるのを防ぐため。
  EOT
  type        = map(string)
}

variable "function_names_secondary" {
  description = "セカンダリ（us-east-1）の関数キー => Lambda 関数名の map。module.lambda_secondary の function_names output を渡す。"
  type        = map(string)
}

variable "primary_region" {
  description = "プライマリリージョン。ダッシュボード本体が作られる場所であり、ウィジェットの既定 region にもなる。"
  type        = string
  default     = "ap-northeast-1"
}

variable "secondary_region" {
  description = <<-EOT
    セカンダリリージョン。メトリクスごとに region を上書きして 1 つのダッシュボードに
    両リージョンを並べるために使う（アラームはリージョンを横断できないが、
    ダッシュボードのウィジェットは region を指定できる）。
  EOT
  type        = string
  default     = "us-east-1"
}

variable "latency_threshold_ms" {
  description = "レイテンシグラフに引く水平線の値（ミリ秒）。負荷テストの合格基準（API Gateway Latency の p99 < 1000ms）を目で判定できるようにするためのもの。"
  type        = number
  default     = 1000
}

variable "period" {
  description = <<-EOT
    メトリクスの集計期間（秒）。折れ線グラフの粒度として効く。
    singleValue のウィジェットは setPeriodToTimeRange = true を指定しており、
    画面の表示期間で集計するためこの値に影響されない。
  EOT
  type        = number
  default     = 60
}

variable "table_names" {
  description = "DynamoDB テーブル名のリスト。"
  type        = list(string)
}


variable "rest_api_name_primary" {
  description = <<-EOT
    プライマリの REST API **名**（例 raidync-dev-api）。module.api_gateway の
    rest_api_name output を渡す。CloudWatch の ApiName ディメンションに使う。

    **rest_api_id（0eyvmklmfb のような ID）を渡さないこと。**ID を持つメトリクスは
    存在しないため、エラーにならずグラフが空になるだけで気づけない。
  EOT
  type        = string
}
variable "rest_api_name_secondary" {
  description = "セカンダリの REST API 名。module.api_gateway_secondary の rest_api_name output を渡す。両リージョンで同名（raidync-dev-api）だが、region の指定で別物として扱われる。"
  type        = string
}
variable "stage_name" {
  description = "CloudWatch の Stage ディメンションの値。module.api_gateway / api_gateway_secondary に渡している stage_name と一致させること（ずれるとメトリクスに一致せず空になる）。"
  type        = string
  default     = "dev"
}
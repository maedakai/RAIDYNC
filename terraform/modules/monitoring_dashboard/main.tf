# 負荷テスト（6-6）の合否を 1 画面で判定するためのダッシュボード。
#
# y=0 の行が**合否そのもの**（docs/qa-log.md §15「負荷テストの合格基準」の 5 項目に対応）、
# y=4 以降は**基準を割ったときの原因切り分け**用に並べている。
#
# アラームはリージョンをまたげないので modules/monitoring は両リージョンに 2 セット作るが、
# ダッシュボードのウィジェットはメトリクスごとに region を指定できるため、ここでは
# 1 つのダッシュボードに東京と us-east-1 を同居させている。
resource "aws_cloudwatch_dashboard" "this" {
  dashboard_name = var.dashboard_name
  dashboard_body = jsonencode({
    widgets = [
      # 合格基準①「HandlerError 0 件」。EMF が出す全関数 × 両リージョンを 1 つの数字に畳む。
      # ⚠ `--`（データ無し）と `0` は画面上で区別できない。CloudWatch には「系列が 1 本も
      #    存在しない状態から 0 の系列を作る」手段が無いため。右下の Invocations に値が
      #    出ているかと**必ず併せて読む**こと（出ていなければ 0 件ではなく判定不能）。
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 6
        height = 4
        properties = {
          view                 = "singleValue"
          stat                 = "Sum"
          period               = var.period
          region               = var.primary_region
          title                = "Handler Errors(合計・０であるべき)"
          setPeriodToTimeRange = true
          metrics = concat(
            # ① 東京の全関数。合計の材料なので個別には表示しない。
            [for k, name in var.function_names_primary : [
              "RAIDYNC/Handlers", "HandlerError", "FunctionName", name,
              { id = "p_${k}", region = var.primary_region, visible = false }
            ]],

            # ② us-east-1 の全関数。region を上書きして同じウィジェットに載せる。
            [for k, name in var.function_names_secondary : [
              "RAIDYNC/Handlers", "HandlerError", "FunctionName", name,
              { id = "s_${k}", region = var.secondary_region, visible = false }
            ]],

            # ③ 上の全メトリクスを合計し、この 1 つだけを数値として表示する。
            [[{ expression = "SUM(METRICS())", label = "HandlerError 合計", id = "e1" }]]
          )

        }
      },
      # 合格基準③「詰まり」の片方。Lambda の同時実行はアカウント単位のクォータ（既定 1,000）
      # で制限されるため、1,000 セッション＝4,000 接続の負荷では現実的に当たりうる。
      # ここが 0 でなければ、遅延の原因は実装ではなくクォータ側にある。
      {
        type   = "metric"
        x      = 6
        y      = 0
        width  = 6
        height = 4
        properties = {
          view                 = "singleValue"
          stat                 = "Sum"
          period               = var.period
          region               = var.primary_region
          title                = "Lambda Throttles(合計・０であるべき)"
          setPeriodToTimeRange = true
          metrics = concat(
            # ① 東京の全関数。合計の材料なので個別には表示しない。
            [for k, name in var.function_names_primary : [
              "AWS/Lambda", "Throttles", "FunctionName", name,
              { id = "p_${k}", region = var.primary_region, visible = false }
            ]],

            # ② us-east-1 の全関数。region を上書きして同じウィジェットに載せる。
            [for k, name in var.function_names_secondary : [
              "AWS/Lambda", "Throttles", "FunctionName", name,
              { id = "s_${k}", region = var.secondary_region, visible = false }
            ]],

            # ③ 上の全メトリクスを合計し、この 1 つだけを数値として表示する。
            [[{ expression = "SUM(METRICS())", label = "Throttles 合計", id = "e1" }]]
          )

        }
      },
      # 合格基準③「詰まり」のもう片方。全テーブルが PAY_PER_REQUEST（自動スケール）なので、
      # ここに値が出たら「オンデマンドのスケーリングが負荷の立ち上がりに追いつかなかった」
      # という意味になる。Read と Write を別メトリクスで取るのは、読み書きのどちらで
      # 詰まったかを後から切り分けられるようにするため。
      # ⚠ DynamoDB はスロットル未発生時に 0 を publish しない。平常時に `--` なのが正常で、
      #    この点は Handler Errors と同じ読み方になる。
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 6
        height = 4
        properties = {
          view                 = "singleValue"
          stat                 = "Sum"
          period               = var.period
          region               = var.primary_region
          title                = "DynamoDB Throttles(合計・０であるべき)"
          setPeriodToTimeRange = true
          metrics = concat(
            # ① 東京の全テーブル。合計の材料なので個別には表示しない。
            [for k, name in var.table_names : [
              "AWS/DynamoDB", "WriteThrottleEvents", "TableName", name,
              { id = "pw_${name}", region = var.primary_region, visible = false }
            ]],
            [for k, name in var.table_names : [
              "AWS/DynamoDB", "ReadThrottleEvents", "TableName", name,
              { id = "pr_${name}", region = var.primary_region, visible = false }
            ]],
            # ② us-east-1 の全テーブル。Global Tables のレプリカも Tickets も
            #    名前は東京と同じで、region の上書きだけが両者を分ける。
            [for k, name in var.table_names : [
              "AWS/DynamoDB", "WriteThrottleEvents", "TableName", name,
              { id = "sw_${name}", region = var.secondary_region, visible = false }
            ]],
            [for k, name in var.table_names : [
              "AWS/DynamoDB", "ReadThrottleEvents", "TableName", name,
              { id = "sr_${name}", region = var.secondary_region, visible = false }
            ]],
            # ③ 上の全メトリクスを合計し、この 1 つだけを数値として表示する。
            [[{ expression = "SUM(METRICS())", label = "ThrottleEvents 合計", id = "e1" }]]
          )

        }
      },
      # 合格基準④「TIMEOUT が投入人数から計算した期待値ちょうど」。MinMatchSize = 2 なので
      # 余りが 2〜3 人なら見切り発車で成立し、**余りが 1 のときだけ 1 件**出る。
      # リージョンごとに式を分けているのは SEARCH がリージョンをまたげないため
      # （上の 3 枚のような 1 本の SUM にはできない）。
      {
        type   = "metric"
        x      = 18
        y      = 0
        width  = 6
        height = 4
        properties = {
          view                 = "singleValue"
          stat                 = "Sum"
          period               = var.period
          region               = var.primary_region
          title                = "MatchTimeout(期待値と一致すること)"
          setPeriodToTimeRange = true
          metrics = [
            [{
              expression = "SUM(SEARCH('{RAIDYNC/Matchmaking,QuestID} MetricName=\"MatchTimeout\"', 'Sum'))"
              id         = "e_apne1"
              label      = "東京"
              region     = var.primary_region
            }],
            [{
              expression = "SUM(SEARCH('{RAIDYNC/Matchmaking,QuestID} MetricName=\"MatchTimeout\"', 'Sum'))"
              id         = "e_use1"
              label      = "us-east-1"
              region     = var.secondary_region
            }],
          ]

        }
      },
      # 合格基準⑤「成立セッションがすべて 4 人」。PartySize ごとに線が立つので、2〜3 人の
      # 山が出たら「負荷でマッチングが遅れ、60 秒の見切り発車で成立した」ことを意味する。
      # TIMEOUT 数だけでは見えない失敗モード（2 人パーティばかりでも TIMEOUT は 0 になる）を
      # ここで拾う。
      # SEARCH の `{名前空間,PartySize}` は**ディメンションが PartySize ちょうど 1 つ**の
      # メトリクスにだけ一致するため、EMF がもう 1 段宣言している [QuestID, PartySize] とは
      # 二重カウントにならない。
      {
        type   = "metric"
        x      = 0
        y      = 4
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          stat   = "Sum"
          period = var.period
          region = var.primary_region
          title  = "MatchFormed(東京)"
          metrics = [
            [{
              expression = "SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchFormed\"', 'Sum')"
              id         = "e_apne1"
              region     = var.primary_region
            }],
          ]
        }
      },
      # 東京側と同じ。SEARCH はリージョンをまたげないのでウィジェットごと分けている。
      {
        type   = "metric"
        x      = 12
        y      = 4
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          stat   = "Sum"
          period = var.period
          region = var.secondary_region
          title  = "MatchFormed(us-east-1)"
          metrics = [
            [{
              expression = "SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchFormed\"', 'Sum')"
              id         = "e_use1"
              region     = var.secondary_region
            }],
          ]
        }
      },
      # 待ち時間の劣化を見る（合格基準そのものではなく、⑤が崩れたときの原因側）。
      # ⚠ `MAX(SEARCH(...))` は「**PartySize ごとに出した p95 のうち最大のもの**」であって
      #    全体の p95 ではない。EMF が PartySize を含まない集計セットを宣言していない以上
      #    これが唯一の手段になる。負荷テストは soloAllowed=false で回すため母集団が
      #    PartySize=4 だけになり、実質は全体の p95 と一致する。
      # p95 と Maximum を並べるのは、外れ値 1 件なのか全体が遅いのかを区別するため。
      {
        type   = "metric"
        x      = 0
        y      = 8
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.primary_region
          title  = "MatchWaitTime(東京)"
          metrics = [
            [{
              expression = "MAX(SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchWaitTime\"', 'p95'))"
              id         = "e_apne1_p95"
              label      = "p95"
              region     = var.primary_region
            }],
            [{
              expression = "MAX(SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchWaitTime\"', 'Maximum'))"
              id         = "e_apne1_maximum"
              label      = "Maximum"
              region     = var.primary_region
            }]
          ]
        }
      },
      # 東京側と同じ。
      {
        type   = "metric"
        x      = 12
        y      = 8
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.secondary_region
          title  = "MatchWaitTime(us-east-1)"
          metrics = [
            [{
              expression = "MAX(SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchWaitTime\"', 'p95'))"
              id         = "e_use1_p95"
              label      = "p95"
              region     = var.secondary_region
            }],
            [{
              expression = "MAX(SEARCH('{RAIDYNC/Matchmaking,PartySize} MetricName=\"MatchWaitTime\"', 'Maximum'))"
              id         = "e_use1_maximum"
              label      = "Maximum"
              region     = var.secondary_region
            }]
          ]
        }
      },
      # 合格基準②「API Gateway Latency の p99 < 1,000ms」。水平線がそのしきい値で、
      # 線を超えた時間帯があるかを目で判定する。
      # Count を右軸に同居させるのは単位が違うからだが、目的は**負荷が実際に掛かっていた
      # 時間帯を同じ画面で確認する**こと（Latency 単体だと無風の時間も低い値に見える）。
      # ApiName + Stage は**ステージ集計の標準メトリクス**なので、有料のメソッド別詳細
      # メトリクスを有効にしなくても出る（有効化は 6-6 直前の opt-in に留める）。
      {
        type   = "metric"
        x      = 0
        y      = 12
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.primary_region
          title  = "API Latency / Count(東京)"

          annotations = {
            horizontal = [{ label = "p99 目標", value = var.latency_threshold_ms }]
          }

          metrics = [
            ["AWS/ApiGateway", "Latency", "ApiName", var.rest_api_name_primary, "Stage", var.stage_name,
            { stat = "p99", label = "Latency p99", region = var.primary_region }],
            ["AWS/ApiGateway", "Count", "ApiName", var.rest_api_name_primary, "Stage", var.stage_name,
            { stat = "Sum", label = "リクエスト数", yAxis = "right", region = var.primary_region }],
          ]
        }
      },
      # 東京側と同じ。API 名は両リージョンで同一（raidync-dev-api）で、region の指定だけが
      # 両者を分ける。
      {
        type   = "metric"
        x      = 12
        y      = 12
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.secondary_region
          title  = "API Latency / Count(us-east-1)"

          annotations = {
            horizontal = [{ label = "p99 目標", value = var.latency_threshold_ms }]
          }

          metrics = [
            ["AWS/ApiGateway", "Latency", "ApiName", var.rest_api_name_secondary, "Stage", var.stage_name,
            { stat = "p99", label = "Latency p99", region = var.secondary_region }],
            ["AWS/ApiGateway", "Count", "ApiName", var.rest_api_name_secondary, "Stage", var.stage_name,
            { stat = "Sum", label = "リクエスト数", yAxis = "right", region = var.secondary_region }],
          ]
        }
      },
      # API の p99 が基準を割ったときに「Lambda 側が遅いのか」を見る。
      # **ディメンションを 1 つも指定していない**のは意図的で、AWS/Lambda がリージョン単位で
      # publish している集計メトリクス（コンソールの "Across All Functions"）を見ている。
      # 関数ごとの p99 を SUM で畳むことはできない（パーセンタイルは加算できる統計量では
      # ないため）ので、「全関数まとめた真の p99」を出すにはこの形しかない。
      # ⚠ この方式は**母集団が「このアカウント・このリージョンの全 Lambda」**である。
      #    RAIDYNC 以外の関数が同じアカウントに増えると、このウィジェットの意味が
      #    エラーも警告も無く静かに変わる。
      # 関数別の Duration はメトリクスとして常に存在するので、どの関数が遅いかの切り分けは
      # メトリクスコンソール側で行う（ダッシュボードに 12 本並べると合否が読めなくなる）。
      {
        type   = "metric"
        x      = 0
        y      = 16
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.primary_region
          title  = "Lambda Duration p99(全関数)"
          metrics = [
            ["AWS/Lambda", "Duration",
            { stat = "p99", label = "東京", region = var.primary_region }],
            ["AWS/Lambda", "Duration",
            { stat = "p99", label = "us-east-1", region = var.secondary_region }],
          ]
        }
      },
      # 上と同じくディメンション無し＝全関数の合計。合否そのものではなく、
      # **Handler Errors / DynamoDB Throttles の `--` を「事象ゼロ」と読んでよいかの判断材料**。
      # ここに値が出ていればテストは確かに流れていたと言えるので `--` = 0 件と読める。
      # 出ていなければ、それらの `--` は「計装が動いていない」可能性を含む判定不能な状態。
      {
        type   = "metric"
        x      = 12
        y      = 16
        width  = 12
        height = 4
        properties = {
          view   = "timeSeries"
          period = var.period
          region = var.primary_region
          title  = "Lambda Invocations 合計(全関数)"
          metrics = [
            ["AWS/Lambda", "Invocations",
            { stat = "Sum", label = "東京", region = var.primary_region }],
            ["AWS/Lambda", "Invocations",
            { stat = "Sum", label = "us-east-1", region = var.secondary_region }],
          ]
        }
      }
    ]
  })
}

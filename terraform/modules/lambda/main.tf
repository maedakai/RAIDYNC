# RAIDYNC の Lambda 関数群（Go / provided.al2023 / arm64）。
#
# パッケージング方式: terraform の外（scripts/build-lambdas.sh）で Go をクロスビルドし、
# archive_file で zip 化して渡す。terraform に local-exec ビルドを埋め込むより、
# Windows/CI どちらからも同じ手順で回せる点を優先した。
#
# 注意: AWS_REGION は Lambda ランタイムが自動設定する予約環境変数のため、ここでは設定しない。
#       DYNAMODB_ENDPOINT も未設定にする（空 = 実 AWS。設定すると LocalStack 向けになる）。

locals {
  # cmd/lambda/<name> と 1:1。role と環境変数だけが関数ごとに異なる。
  functions = {
    authorizer = {
      role_arn = var.authorizer_role_arn
      env = {
        AUTH_JWKS_URL  = var.auth_jwks_url
        AUTH_ISSUER    = var.auth_issuer
        AUTH_CLIENT_ID = var.auth_client_id
      }
    }
    # API_BASE_URL はセッションアフィニティ用のリージョン固定 URL。
    # 空なら Lambda がリクエストから導出する（変数の説明を参照）。
    match_request = {
      role_arn = var.matchmaking_role_arn
      env = {
        API_BASE_URL = var.api_base_url
      }
    }
    # マッチ成立時に実 GameLift へセッションを作るのは match_status のみ
    # （matcher が status ポーリング契機でマッチングを実行するため）。
    match_status = {
      role_arn = var.matchmaking_role_arn
      env = {
        GAMELIFT_FLEET_ID = var.gamelift_fleet_id
        GAMELIFT_LOCATION = var.gamelift_location
      }
    }
    match_cancel = {
      role_arn = var.matchmaking_role_arn
      env      = {}
    }
    match_history = {
      role_arn = var.matchmaking_role_arn
      env      = {}
    }
    session_end = {
      role_arn = var.matchmaking_role_arn
      env      = {}
    }
  }

  # 全関数に共通の環境変数。関数ごとの env と merge し、同じキーなら関数ごとの値が勝つ。
  # TRACE_EXPORTER は Span の送り先（internal/telemetry の newExporter）。Lambda は AWS でしか動かないため
  # 常に xray で、変数にしない（未設定だと gRPC で localhost に送ろうとして Span が捨てられる）。
  common_env = {
    TRACE_EXPORTER = "xray"
  }

  # 空文字の環境変数は「未設定」と同義なので落とす。
  # （コード側は Getenv の空文字で分岐するため、空を渡しても害はないがノイズになる）
  functions_resolved = {
    for name, cfg in local.functions : name => merge(cfg, {
      env = { for k, v in merge(local.common_env, cfg.env) : k => v if v != "" }
    })
  }
}

# このモジュールがどのリージョンに適用されているかを知るために引く。
# マルチリージョン構成では同じモジュールが provider alias 違いで複数回インスタンス化され、
# zip の出力先が衝突するため、パスにリージョン名を混ぜて分離する。
data "aws_region" "current" {}

# ビルド済み bootstrap を関数ごとに zip 化する。
# 事前に scripts/build-lambdas.sh の実行が必要（未実行だと plan 時に source_dir 不在で失敗）。
data "archive_file" "this" {
  for_each = local.functions

  type        = "zip"
  source_dir  = "${var.artifacts_dir}/${each.key}"
  output_path = "${var.artifacts_dir}/${each.key}-${data.aws_region.current.name}.zip"
}

# ロググループを明示的に作る。Lambda の自動作成に任せると保持期間が「無期限」になり
# 課金が積み上がるため、retention を明示して先に確保する。
resource "aws_cloudwatch_log_group" "this" {
  for_each = local.functions

  name              = "/aws/lambda/${var.name_prefix}-${each.key}"
  retention_in_days = var.log_retention_days
  tags              = var.tags
}

resource "aws_lambda_function" "this" {
  for_each = local.functions_resolved

  function_name = "${var.name_prefix}-${each.key}"
  role          = each.value.role_arn

  # カスタムランタイム。実行ファイル名は bootstrap 固定、handler 値自体は使われない。
  runtime       = "provided.al2023"
  handler       = "bootstrap"
  architectures = ["arm64"]

  filename         = data.archive_file.this[each.key].output_path
  source_code_hash = data.archive_file.this[each.key].output_base64sha256

  memory_size = var.memory_size
  timeout     = var.timeout

  # 環境変数が 1 つも無い関数では environment ブロック自体を出さない。
  dynamic "environment" {
    for_each = length(each.value.env) > 0 ? [each.value.env] : []
    content {
      variables = environment.value
    }
  }

  tracing_config {
    mode = "Active"
  }

  # ロググループを Lambda より先に作らせる（Lambda 側の自動作成と競合させない）。
  depends_on = [aws_cloudwatch_log_group.this]

  tags = var.tags
}

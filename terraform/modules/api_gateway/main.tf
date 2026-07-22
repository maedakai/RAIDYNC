# REST API。ルート構成は cmd/localapi/main.go のマウントと 1:1 で対応させる
# （ローカル E2E と同じ URL 形状にして、クライアント側の差分をベース URL だけにするため）。
#
#   POST   /match/request              -> match_request   (要 JWT)
#   GET    /match/status/{ticketId}    -> match_status    (要 JWT)
#   DELETE /match/cancel/{ticketId}    -> match_cancel    (要 JWT)
#   GET    /player/{playerId}/history  -> match_history   (要 JWT)
#   POST   /session/{sessionId}/end    -> session_end     (認証なし)
#
# 統合はすべて AWS_PROXY（Lambda プロキシ）。パスパラメータは pathParameters として
# そのままハンドラに渡るため、request_parameters のマッピングは不要。

resource "aws_api_gateway_rest_api" "this" {
  name        = "${var.name_prefix}-api"
  description = "RAIDYNC マッチメイキング API"

  endpoint_configuration {
    types = ["REGIONAL"]
  }

  tags = var.tags
}

# ---- TOKEN 型カスタムオーソライザ ----
# authorizer Lambda が Authorization ヘッダの Bearer JWT を検証し、sub を
# Authorizer Context に載せる。業務 Lambda は apigw.PlayerID 経由でそれを読む。
resource "aws_api_gateway_authorizer" "this" {
  name                             = "${var.name_prefix}-jwt"
  rest_api_id                      = aws_api_gateway_rest_api.this.id
  type                             = "TOKEN"
  identity_source                  = "method.request.header.Authorization"
  authorizer_uri                   = var.lambda_invoke_arns["authorizer"]
  authorizer_result_ttl_in_seconds = 0 # dev では検証結果をキャッシュしない（挙動を追いやすくする）
}

# API Gateway が authorizer Lambda を呼べるようにする。
# source_arn を /authorizers/* にしておくことで authorizer リソースとの循環参照を避ける。
resource "aws_lambda_permission" "authorizer" {
  statement_id  = "AllowAPIGatewayInvokeAuthorizer"
  action        = "lambda:InvokeFunction"
  function_name = var.lambda_function_names["authorizer"]
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/authorizers/*"
}

# ---- リソース階層 ----
# terraform では親子関係を動的に組み立てにくいため、階層は明示的に定義する。

resource "aws_api_gateway_resource" "match" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "match"
}

resource "aws_api_gateway_resource" "match_request" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.match.id
  path_part   = "request"
}

resource "aws_api_gateway_resource" "match_status" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.match.id
  path_part   = "status"
}

resource "aws_api_gateway_resource" "match_status_ticket" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.match_status.id
  path_part   = "{ticketId}"
}

resource "aws_api_gateway_resource" "match_cancel" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.match.id
  path_part   = "cancel"
}

resource "aws_api_gateway_resource" "match_cancel_ticket" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.match_cancel.id
  path_part   = "{ticketId}"
}

resource "aws_api_gateway_resource" "player" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "player"
}

resource "aws_api_gateway_resource" "player_id" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.player.id
  path_part   = "{playerId}"
}

resource "aws_api_gateway_resource" "player_history" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.player_id.id
  path_part   = "history"
}

resource "aws_api_gateway_resource" "session" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "session"
}

resource "aws_api_gateway_resource" "session_id" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.session.id
  path_part   = "{sessionId}"
}

resource "aws_api_gateway_resource" "session_end" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_resource.session_id.id
  path_part   = "end"
}

# ---- メソッド / 統合 ----

locals {
  endpoints = {
    match_request = {
      resource_id = aws_api_gateway_resource.match_request.id
      http_method = "POST"
      authorized  = true
    }
    match_status = {
      resource_id = aws_api_gateway_resource.match_status_ticket.id
      http_method = "GET"
      authorized  = true
    }
    match_cancel = {
      resource_id = aws_api_gateway_resource.match_cancel_ticket.id
      http_method = "DELETE"
      authorized  = true
    }
    match_history = {
      resource_id = aws_api_gateway_resource.player_history.id
      http_method = "GET"
      authorized  = true
    }
    # ゲームサーバ（GameLift プロセス）が呼ぶ server-to-server 経路。
    # プレイヤー JWT を持たないため authorizer を通さない。
    session_end = {
      resource_id = aws_api_gateway_resource.session_end.id
      http_method = "POST"
      authorized  = false
    }
  }
}

resource "aws_api_gateway_method" "this" {
  for_each = local.endpoints

  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = each.value.resource_id
  http_method   = each.value.http_method
  authorization = each.value.authorized ? "CUSTOM" : "NONE"
  authorizer_id = each.value.authorized ? aws_api_gateway_authorizer.this.id : null
}

resource "aws_api_gateway_integration" "this" {
  for_each = local.endpoints

  rest_api_id = aws_api_gateway_rest_api.this.id
  resource_id = each.value.resource_id
  http_method = aws_api_gateway_method.this[each.key].http_method

  type = "AWS_PROXY"
  # Lambda 統合は HTTP メソッドに関わらず POST で呼び出す（AWS 仕様）。
  integration_http_method = "POST"
  uri                     = var.lambda_invoke_arns[each.key]
}

# 各業務 Lambda を API Gateway から呼べるようにする。
resource "aws_lambda_permission" "this" {
  for_each = local.endpoints

  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = var.lambda_function_names[each.key]
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}

# ---- デプロイ / ステージ ----

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id

  # メソッド・統合・オーソライザの変更を検知して再デプロイする。
  # これが無いと構成を変えても古いスナップショットが配信され続ける。
  triggers = {
    redeployment = sha1(jsonencode([
      aws_api_gateway_authorizer.this.id,
      [for k, m in aws_api_gateway_method.this : "${k}:${m.http_method}:${m.authorization}"],
      [for k, i in aws_api_gateway_integration.this : "${k}:${i.uri}"],
    ]))
  }

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_api_gateway_stage" "this" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = var.stage_name

  tags = var.tags
}

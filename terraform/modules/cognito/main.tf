# Amazon Cognito User Pool。ローカルの mock-auth を実サービスに置き換えるもの。
#
# authorizer Lambda は internal/auth.Verifier で JWKS を取得し RS256 を検証する。
# mock-auth と同じ検証経路（JWKS / iss / exp / aud|client_id）がそのまま実 Cognito に効く。
#
# 設定値の根拠は docs/spec-architecture.md §2.3。

data "aws_region" "current" {}

resource "aws_cognito_user_pool" "this" {
  name = "${var.name_prefix}-users"

  # メール + パスワード認証。メールアドレスをユーザー名として使う。
  username_attributes      = ["email"]
  auto_verified_attributes = ["email"]

  password_policy {
    minimum_length    = 8
    require_lowercase = true
    require_uppercase = true
    require_numbers   = true
    require_symbols   = true
  }

  # MFA は「オプション（TOTP）」。テストユーザーは TOTP 未登録のため
  # 追加チャレンジ無しでログインでき、E2E の妨げにならない。
  mfa_configuration = "OPTIONAL"
  software_token_mfa_configuration {
    enabled = true
  }

  tags = var.tags
}

resource "aws_cognito_user_pool_client" "this" {
  name         = "${var.name_prefix}-client"
  user_pool_id = aws_cognito_user_pool.this.id

  # パブリッククライアント（シークレット無し）。secret を付けると InitiateAuth に
  # SECRET_HASH の計算が必要になり、CLI/Unity からの利用が煩雑になるため。
  generate_secret = false

  # USER_PASSWORD_AUTH: E2E で CLI から直接ログインするために使う。
  # USER_SRP_AUTH: Unity クライアントを想定した本来の推奨フロー。
  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_USER_SRP_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]

  access_token_validity  = var.access_token_hours
  id_token_validity      = var.access_token_hours
  refresh_token_validity = var.refresh_token_days

  token_validity_units {
    access_token  = "hours"
    id_token      = "hours"
    refresh_token = "days"
  }
}

# テストユーザーのパスワード。リポジトリに平文を置かないため自動生成する。
# 取得は `terraform output -raw cognito_test_user_password`。
# 注意: 生成値は state に平文で保存される（dev のテスト用途に限定しているため許容）。
resource "random_password" "test_user" {
  length  = 20
  upper   = true
  lower   = true
  numeric = true
  special = true

  # password_policy の require_* を確実に満たす。
  min_upper   = 2
  min_lower   = 2
  min_numeric = 2
  min_special = 2

  # Cognito が安全に受け付ける記号に絞る。
  override_special = "!@#$%^&*()_+-="
}

# E2E 用テストユーザー。message_action=SUPPRESS で招待メールを送らず、
# email_verified=true にして検証フローを飛ばす（ドメインは実在しなくてよい）。
resource "aws_cognito_user" "test" {
  count = var.test_user_count

  user_pool_id = aws_cognito_user_pool.this.id
  username     = "player${count.index + 1}@${var.test_user_email_domain}"

  attributes = {
    email          = "player${count.index + 1}@${var.test_user_email_domain}"
    email_verified = true
  }

  # 永続パスワード。temporary_password だと初回に NEW_PASSWORD_REQUIRED
  # チャレンジが発生し、E2E のログインが 1 手増えるため使わない。
  password       = random_password.test_user.result
  message_action = "SUPPRESS"
}

# GameLift Managed EC2 フリート（本番同等）。
#
# Anywhere（environments/anywhere）との違いは compute の調達方法だけでなく、
# 「Build を zip でアップロードし GameLift が各 EC2 へ自動配置する」デプロイ経路が
# ここで初めて登場する点（docs/qa-log.md §11）。SDK 契約自体は Anywhere で検証済み。
#
# ⚠ コスト: フリートは ACTIVE な間ずっとインスタンス時間課金が発生する
#    （プレイヤー 0 でも）。他モジュール（DynamoDB/Lambda/API GW/Cognito）が
#    実質従量課金なのに対し、ここだけ性質が違う。E2E 後は必ず destroy すること。

data "aws_caller_identity" "current" {}

# マルチリージョン構成ではこのモジュールが provider alias 違いで複数回
# インスタンス化される。S3 バケット名（グローバル一意）・IAM ロール名（グローバル）・
# zip の出力パスがそのままだと衝突するため、リージョン名を混ぜて分離する。
data "aws_region" "current" {}

# ---- Build 成果物の zip 化と S3 配置 ----

# scripts/build-gameserver.sh の出力（gameserver / install.sh / start.sh）を固める。
data "archive_file" "build" {
  type       = "zip"
  source_dir = var.build_dir
  # リージョンごとに別ファイルにする（同一パスだと 2 インスタンスが取り合う）。
  output_path = "${var.build_dir}-${data.aws_region.current.name}.zip"
  # Windows で生成したファイルは実行ビットを持たず、そのまま zip 化すると
  # 展開後 gameserver / start.sh が非実行になり GameLift が起動できない
  # （SERVER_PROCESS_INVALID_PATH）。全ファイルを 0755 で固めて実行権限を保証する。
  output_file_mode = "0755"
}

resource "aws_s3_bucket" "build" {
  # バケット名はグローバル一意が必要なためアカウント ID を混ぜる。さらに
  # GameLift の CreateBuild は**フリートと同一リージョンの S3** を要求するため、
  # リージョンごとにバケットを分ける（リージョン名も名前に含める）。
  bucket = "${var.name_prefix}-gamelift-build-${data.aws_caller_identity.current.account_id}-${data.aws_region.current.name}"

  # dev では destroy 時に中身ごと消せるようにする（後片付けを 1 コマンドで完結させる）。
  force_destroy = true

  tags = var.tags
}

# 既定でもブロックされるが、Build 配布用バケットであることを明示する。
resource "aws_s3_bucket_public_access_block" "build" {
  bucket = aws_s3_bucket.build.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# キーに内容ハッシュを含める。ビルドが変わればキーが変わり、
# 下の aws_gamelift_build も作り直される（GameLift の Build は不変のため）。
resource "aws_s3_object" "build" {
  bucket = aws_s3_bucket.build.id
  key    = "gameserver-${data.archive_file.build.output_md5}.zip"
  source = data.archive_file.build.output_path
  etag   = data.archive_file.build.output_md5
}

# ---- GameLift が S3 から Build を読むためのロール ----

data "aws_iam_policy_document" "gamelift_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["gamelift.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "build_access" {
  name               = "${var.name_prefix}-gamelift-build-access-${data.aws_region.current.name}"
  assume_role_policy = data.aws_iam_policy_document.gamelift_assume.json
  tags               = var.tags
}

data "aws_iam_policy_document" "build_access" {
  statement {
    sid       = "ReadBuildObject"
    actions   = ["s3:GetObject", "s3:GetObjectVersion"]
    resources = ["${aws_s3_bucket.build.arn}/*"]
  }
  # CreateBuild 時にオブジェクトの存在確認でバケットを引くことがある。
  statement {
    sid       = "ListBuildBucket"
    actions   = ["s3:GetBucketLocation"]
    resources = [aws_s3_bucket.build.arn]
  }
}

resource "aws_iam_role_policy" "build_access" {
  name   = "${var.name_prefix}-gamelift-build-access-${data.aws_region.current.name}"
  role   = aws_iam_role.build_access.id
  policy = data.aws_iam_policy_document.build_access.json
}

# ---- Build ----

# classic aws_gamelift_build は server_sdk_version 属性が無く、AL2023 × SDK 5.x を作れない。
# awscc（Cloud Control）版なら server_sdk_version を指定できる。storage_location / tags は
# nested block でなく object 属性で、tags は {key,value} のリスト形式である点が aws provider と異なる。
resource "awscc_gamelift_build" "this" {
  name             = "${var.name_prefix}-gameserver-${data.aws_region.current.name}"
  operating_system = var.operating_system
  # AL2023 は SDK 5.0 以上でのみサポートされる（未指定だと既定 4.0.2 で CreateBuild が失敗する）。
  server_sdk_version = var.server_sdk_version
  # 内容が変われば version も変わり、Build が作り直される。
  version = substr(data.archive_file.build.output_md5, 0, 12)

  storage_location = {
    bucket   = aws_s3_bucket.build.id
    key      = aws_s3_object.build.key
    role_arn = aws_iam_role.build_access.arn
  }

  tags = [for k, v in var.tags : { key = k, value = v }]

  # ロールを先に作らないと GameLift が S3 を読めず CreateBuild が失敗する。
  depends_on = [aws_iam_role_policy.build_access]
}

# ---- Fleet ----

resource "aws_gamelift_fleet" "this" {
  name        = "${var.name_prefix}-fleet-${data.aws_region.current.name}"
  description = "RAIDYNC ゲームサーバー（Managed EC2 / ${var.fleet_type}）"

  build_id          = awscc_gamelift_build.this.build_id
  ec2_instance_type = var.ec2_instance_type
  fleet_type        = var.fleet_type

  # ゲームクライアントが UDP で直接繋ぐポートを開ける。
  ec2_inbound_permission {
    from_port = var.game_port
    to_port   = var.game_port
    ip_range  = var.allowed_ip_range
    protocol  = "UDP"
  }

  runtime_configuration {
    server_process {
      # start.sh が環境変数を export してから gameserver を exec する。
      # Build zip のルートが /local/game に展開される。
      launch_path = "/local/game/start.sh"
      # start.sh の第 1 引数。ゲームサーバーがセッション終了時に /session/end を呼ぶ API のベース URL。
      # runtime configuration は環境変数を渡せないため、引数で渡して start.sh が SESSION_API_URL に入れる。
      parameters = var.session_api_url
      # gameserver は固定ポート(7777)を listen するため、1 インスタンス 1 プロセス。
      concurrent_executions = 1
    }
  }

  # dev では実行中セッションを保護しない（Spot 中断やフリート整理を妨げないため）。
  new_game_session_protection_policy = "NoProtection"

  tags = var.tags
}

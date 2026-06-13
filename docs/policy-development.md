# 開発方針

---

## 1. ローカル開発環境方針

### 1.1 概要

AWSへの毎回デプロイによる開発コスト・時間ロスを防ぐため、Dockerベースのローカル環境を整備する。`docker compose up` 一発で全サービスが立ち上がる構成とし、チーム開発への対応力もアピールする。

### 1.2 ローカル環境全体構成

```
[ Unity Editor（クライアント）]
         │
         ▼
[ Docker Compose（ローカル）]
  ├── LocalStack            ← AWSサービスをローカルで再現
  │    ├── DynamoDB
  │    ├── API Gateway
  │    └── Lambda
  ├── Amazon GameLift Local ← GameLiftのローカルエミュレーター（AWS公式）
  └── Mock Server           ← Cognito認証のモック
```

### 1.3 使用ツール一覧

| ツール | 役割 | 備考 |
|-------|------|------|
| **Docker / Docker Compose** | 環境構築の統一 | `docker compose up`で全サービス起動 |
| **LocalStack** | AWSサービスのローカル再現 | DynamoDB・Lambda・API Gatewayをオフラインで動かす |
| **Amazon GameLift Local** | GameLiftのローカルエミュレーター | AWS公式提供ツール |
| **k6** | 負荷テスト | ローカル接続テストにも使用 |

### 1.4 Docker Compose構成

```yaml
version: "3.9"
services:

  localstack:
    image: localstack/localstack
    ports:
      - "4566:4566"
    environment:
      - SERVICES=dynamodb,lambda,apigateway,s3
      - DEFAULT_REGION=ap-northeast-1
    volumes:
      - ./localstack/init:/etc/localstack/init/ready.d

  gamelift-local:
    image: amazonlinux:2
    ports:
      - "7777:7777"   # ゲームポート（UDP）
      - "8080:8080"   # GameLift Local管理ポート
    volumes:
      - ./gamelift-local:/gamelift
    command: >
      java -jar GameLiftLocal.jar -p 8080

  mock-auth:
    image: node:20-alpine
    ports:
      - "3000:3000"
    volumes:
      - ./mock-auth:/app
    command: node /app/server.js
```

### 1.5 環境変数管理

`.env.local`（ローカル）と`.env.dev`（AWS dev）を分離して管理する。

```bash
# .env.local（ローカル開発用）
AWS_ENDPOINT=http://localhost:4566
AWS_REGION=ap-northeast-1
AWS_ACCESS_KEY_ID=dummy
AWS_SECRET_ACCESS_KEY=dummy
DYNAMODB_TABLE_PLAYERS=Players
DYNAMODB_TABLE_SESSIONS=Sessions
GAMELIFT_ENDPOINT=http://localhost:8080
AUTH_ENDPOINT=http://localhost:3000

# .env.dev（AWS dev環境用）
AWS_REGION=ap-northeast-1
DYNAMODB_TABLE_PLAYERS=Players-dev
DYNAMODB_TABLE_SESSIONS=Sessions-dev
COGNITO_USER_POOL_ID=ap-northeast-1_xxxxxxxx
```

> `.env.local`はGitignoreに追加し、`.env.example`をリポジトリに含める。

### 1.6 環境の使い分け方針

| 作業内容 | 使用環境 |
|---------|---------|
| Lambdaのロジック修正・デバッグ | ローカル |
| DynamoDBのCRUD確認 | ローカル（LocalStack） |
| Unity ↔ サーバー間の接続確認 | ローカル（GameLift Local） |
| マッチメイキングAPIの動作確認 | ローカル |
| マルチリージョン動作確認 | AWS dev |
| 大規模負荷テスト（1,000接続） | AWS dev |
| 最終デモ・面接提示 | AWS prod |

### 1.7 テスト戦略

| テスト種別 | 実行環境 | ツール | 対象 |
|-----------|---------|--------|------|
| **単体テスト** | ローカル | Go test | Lambdaのロジック・DynamoDB操作 |
| **結合テスト** | ローカル（LocalStack） | Go test | API Gateway → Lambda → DynamoDB |
| **接続テスト** | ローカル（GameLift Local） | Goテストクライアント | クライアント ↔ GameLiftサーバー間通信 |
| **負荷テスト（小）** | ローカル | k6 | 10〜50接続の動作確認 |
| **負荷テスト（大）** | AWS dev | k6 | 1,000接続の本番想定テスト |
| **E2Eテスト** | AWS dev | 手動 + k6 | 全フロー（ログイン〜マッチ〜ゲーム終了） |

### 1.8 ローカル環境の起動手順

```bash
# 1. リポジトリのクローン
git clone https://github.com/yourname/raidync.git
cd raidync

# 2. 環境変数の設定
cp .env.example .env.local

# 3. Dockerコンテナ起動
docker compose up -d

# 4. LocalStackの初期化（DynamoDBテーブル作成等）
./scripts/localstack-init.sh

# 5. 単体テスト実行
cd server && go test ./...
```

### 1.9 リポジトリ追加ファイル構成

```
/
├── docker-compose.yml           # ローカル環境定義
├── .env.example                 # 環境変数テンプレート
├── .gitignore                   # .env.local等を除外
├── scripts/
│   ├── localstack-init.sh       # LocalStack初期化スクリプト
│   └── gamelift-local-start.sh  # GameLift Local起動スクリプト
├── mock-auth/
│   └── server.js                # Cognito認証モックサーバー
└── localstack/
    └── init/                    # LocalStack起動時の自動初期化スクリプト
```

---

## 2. CI/CD方針

### 2.1 パイプライン構成

```
GitHub Push（mainブランチ）
         │
         ▼
GitHub Actions
  ・Terraformフォーマットチェック
  ・tflintによる静的解析
  ・Go test実行
         │
         ▼
AWS CodePipeline
  ・Terraform plan 確認
  ・承認ステップ（手動）
  ・Terraform apply
  ・GameLiftフリート更新
```

### 2.2 環境分離

| 環境 | 用途 | GameLiftインスタンス |
|------|------|-------------------|
| dev | 開発・検証 | c5.large × 1〜2 |
| prod | デモ・面接提示用 | c5.large × 2〜5 |

### 2.3 負荷テスト方針

| テスト対象 | ツール | 規模 |
|-----------|--------|------|
| マッチメイキングAPI | k6 | 1,000req/秒 |
| GameLift実接続 | Goヘッドレスクライアント | 50接続 |
| GameLiftスケーリング | AWSコンソール監視 | フリートメトリクス確認 |

#### 段階的負荷テストステップ

```
Step 1：10接続   → 基本動作確認（ローカル）
Step 2：50接続   → ローカル上限テスト（ローカル）
Step 3：100接続  → AWS dev 初回テスト
Step 4：500接続  → オートスケーリング発動確認
Step 5：1,000接続 → 最終目標値確認
```

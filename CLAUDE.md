# CLAUDE.md

このファイルは、リポジトリ内のコードを扱う際に Claude Code (claude.ai/code) へ指示を提供します。

## プロジェクト概要

**RAIDYNC** は CAPCOM（AAAゲーム会社）をターゲットとした AWS インフラポートフォリオプロジェクトです。モンスターハンタースタイルの 4 人協力リアルタイムアクションゲームに向けたスケーラブルなインフラを示します。焦点はゲームの完成度ではなく、インフラエンジニアリングの品質にあります。

- 目標: 同時セッション 1,000（接続数 4,000）
- リージョン: ap-northeast-1（プライマリ）+ us-east-1（セカンダリ）
- ゲームセッション時間: 最大 20 分

## 開発コマンド

> プロジェクトは構築フェーズ中です。以下のコマンドはファイル作成に伴い、計画および実際のセットアップを反映しています。

```bash
# ローカル環境
docker compose -f docker/docker-compose.yml up -d                    # LocalStack + GameLift Local + Mock Auth を起動
./docker/localstack/scripts/localstack-init.sh            # LocalStack リソースを初期化

# Go バックエンド (Lambda + GameLift サーバー)
cd server && go test ./...              # ユニットテスト実行
cd server && go vet ./...               # 静的解析
cd server && go build ./...             # ビルド

# mock-auth (Cognito ローカルモック / Go)
docker compose -f docker/docker-compose.yml up -d mock-auth   # 起動（air でホットリロード）

# Terraform
terraform -chdir=terraform/environments/dev plan
terraform -chdir=terraform/environments/dev apply

# 負荷テスト
k6 run tests/load/load-test-small.js   # 同時ユーザー 10〜50（ローカル）
k6 run tests/load/load-test-large.js   # 同時ユーザー 1,000（AWS dev）
```

## アーキテクチャ

### システムフロー

```
Unity クライアント (PC/Windows)
  → Route 53 (レイテンシールーティング)
  → API Gateway + Lambda (REST: 認証、マッチメイキング)
  → Amazon GameLift (UDP ゲームセッション)
  → DynamoDB (プレイヤーデータ、セッション、マッチ履歴)
```

### AWS サービス

| サービス | 役割 |
|---------|------|
| Cognito | プレイヤー認証（メール/パスワード + オプション TOTP） |
| API Gateway + Lambda (Go / provided.al2023) | マッチメイキング API、セッション管理 |
| Amazon GameLift | 専用ゲームサーバーホスティング；Spot Fleet がプライマリ、On-Demand がフォールバック |
| DynamoDB Global Tables | クロスリージョンデータ（Players、Sessions、MatchHistory） |
| CloudWatch + X-Ray | メトリクス、アラーム、分散トレーシング |

### DynamoDB テーブル

| テーブル | PK | SK | 用途 |
|-------|----|----|---------|
| Players | playerId (String) | — | プレイヤープロファイル |
| Sessions | sessionId (String) | — | アクティブなゲームセッション状態 |
| MatchHistory | playerId (String) | timestamp (Number) | マッチ結果 |
| MatchmakingTickets | ticketId (String) | — | マッチ待機チケット（短命、TTL 90s、region-local。GSI: questId-status, playerId-status） |

### Terraform レイアウト

```
terraform/
├── environments/dev/   # dev tfvars + main.tf
├── environments/prod/  # prod tfvars + main.tf
└── modules/
    ├── vpc/            # VPC 10.0.0.0/16、パブリック/プライベートサブネット
    ├── cognito/
    ├── api_gateway/
    ├── lambda/
    ├── gamelift/
    ├── dynamodb/
    └── monitoring/
```

### ローカル開発スタック (Docker Compose)

- **LocalStack** — DynamoDB、Lambda、API Gateway をエミュレート
- **GameLift Local** — Amazon GameLift 公式エミュレーター
- **Mock Auth Server** — Go 製 Cognito モック（air でホットリロード、ポート 9229）

## 開発フェーズ

プロジェクトは 7 フェーズのロードマップに沿って進行します。仕様は `docs/specification.md` にあります。

| フェーズ | 内容 |
|-------|-------|
| 0 | ローカル環境セットアップ（Docker、LocalStack、GameLift Local） |
| 1 | Terraform コア（VPC、IAM、Cognito、CI/CD） |
| 2 | Lambda + DynamoDB 実装 & ユニットテスト |
| 3 | GameLift + Unity 連携 |
| 4 | マッチメイキング + AWS dev での E2E テスト |
| 5 | マルチリージョン DynamoDB Global Tables |
| 6 | モニタリング、負荷テスト、セキュリティ強化 |
| 7 | ドキュメント整備 & ポートフォリオ公開 |

## 主要制約

- **ローカルファースト**: 開発中は AWS の使用を最小化する。AWS へのデプロイ前に LocalStack と GameLift Local を使用すること。
- **コスト管理**: AWS Budgets アラートを月 $200 に設定。GameLift フリートは使用しないときは必ず停止すること。
- **Spot インスタンス**: GameLift は Spot Fleet を使用（約 70% のコスト削減）；On-Demand はフォールバックのみ。
- **IaC の徹底**: すべての AWS リソースは Terraform で定義すること — コンソールからの手動変更は禁止。

## ドキュメント運用

- **質問ログ**: ユーザーがプロジェクトの内容（設計・実装・インフラ・運用など）について質問したら、その質問と回答の要点を `docs/qa-log.md` に追記すること。トピック別の Q&A 形式（`### Q.` 見出し + 要点回答）を維持し、適切なセクションがなければ新設する。学習ログ兼ポートフォリオ素材として育てる。

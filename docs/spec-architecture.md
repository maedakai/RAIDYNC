# アーキテクチャ・インフラ仕様

---

## 1. システムアーキテクチャ

### 全体構成図

```
[ Unity Client（PC） ]
         │
         ▼
[ Route 53（レイテンシールーティング） ]
         │
  ┌──────┴──────────────┐
  ▼                     ▼
[ ap-northeast-1 ]   [ us-east-1 ]
（東京：メイン）      （バージニア：サブ）
  │                     │
  └──────────┬──────────┘
             │
  ┌──────────▼──────────────────────────┐
  │          共通サービス層              │
  │  Amazon Cognito（認証）              │
  │  API Gateway + Lambda（マッチング）  │
  │  DynamoDB Global Tables             │
  └──────────────────┬──────────────────┘
                     │
  ┌──────────────────▼──────────────────┐
  │        Amazon GameLift              │
  │  Spot Fleet（通常時）               │
  │  On-Demand Fleet（保険用）          │
  │  Auto Scaling（接続数に応じて）     │
  └──────────────────┬──────────────────┘
                     │
  ┌──────────────────▼──────────────────┐
  │        監視・運用層                  │
  │  CloudWatch Dashboard               │
  │  X-Ray（分散トレーシング）           │
  │  CloudWatch Alarms → SNS → 通知     │
  └─────────────────────────────────────┘
```

### リージョン構成

| リージョン | 役割 | 優先度 |
|-----------|------|--------|
| ap-northeast-1（東京） | メインリージョン | Primary |
| us-east-1（バージニア） | サブリージョン | Secondary |

---

## 2. AWSインフラ仕様

### 2.1 ネットワーク（VPC）

> ⚠️ **不採用（2026-07-22 決定）。** 以下は計画時の設計であり、**実際には構築しない**。
> RAIDYNC は サーバーレス（Lambda）+ GameLift マネージド構成であり、Lambda は DynamoDB・
> GameLift をパブリックエンドポイントで呼べ、GameLift Managed EC2 は GameLift 側が管理する
> VPC で動くため、自前 VPC の使い所が無い。Lambda を VPC に入れると外部通信に NAT Gateway が
> 必要になり、下表どおり各 AZ 1台＝2台で月 $60 前後の時間課金が乗るだけになる。
> 判断の詳細は docs/qa-log.md §12。将来 RDS/ElastiCache 等 VPC 内でしか動かないリソースを
> 追加する場合はこの設計を再利用する。

| 項目 | 設定値（計画時・未構築） |
|------|--------|
| VPC CIDR | 10.0.0.0/16 |
| パブリックサブネット | 10.0.1.0/24, 10.0.2.0/24（マルチAZ） |
| プライベートサブネット | 10.0.10.0/24, 10.0.20.0/24（マルチAZ） |
| NAT Gateway | 各AZに1台 |
| Internet Gateway | 1台 |

### 2.2 Amazon GameLift

| 項目 | 設定値 |
|------|--------|
| フリートタイプ | Spot Fleet（メイン） + On-Demand Fleet（バックアップ） |
| インスタンスタイプ | c5.large |
| 最小インスタンス数 | 1 |
| 最大インスタンス数 | 20 |
| スケーリングポリシー | 同時接続数 > 80%でスケールアウト |
| スケールアウト時間目標 | 3分以内 |
| ゲームポート | UDP 7777 |

### 2.3 Amazon Cognito

| 項目 | 設定値 |
|------|--------|
| ユーザープール | 1プール |
| 認証方式 | メール + パスワード |
| MFA | オプション（TOTP） |
| トークン有効期限 | アクセストークン：1時間 / リフレッシュトークン：30日 |

### 2.4 API Gateway + Lambda

| 項目 | 設定値 |
|------|--------|
| APIタイプ | REST API |
| 認証 | Cognito Authorizer |
| Lambdaランタイム | Go（aws-lambda-go） |
| タイムアウト | 30秒 |
| メモリ | 256MB |

### 2.5 DynamoDB

| テーブル名 | パーティションキー | ソートキー | 用途 |
|-----------|-----------------|----------|------|
| Players | playerId（String） | - | プレイヤー情報 |
| Sessions | sessionId（String） | - | ゲームセッション情報（questId / players 等を属性として保持） |
| MatchHistory | playerId（String） | timestamp（Number） | マッチ履歴（questId / result / durationSec を属性として保持） |
| MatchmakingTickets | ticketId（String） | - | マッチ待機チケット（短命・region-local・TTL 90 秒） |

#### MatchmakingTickets の GSI

| GSI 名 | PK | SK | 用途 |
|--------|----|----|----|
| questId-status-index | questId | status | マッチャーが同 quest の WAITING チケットを抽出 |
| playerId-status-index | playerId | status | 1 プレイヤー 1 チケット制約の冪等性検証 |

- **Global Tables：** Players / Sessions / MatchHistory のみ対象。MatchmakingTickets は region-local（レプリケーション不要）。
- **課金モード：** オンデマンド
- **TTL：** MatchmakingTickets のみ `ttl` 属性で自動削除有効化

### 2.6 CloudFront ※不採用（Phase 5 で判断）

**当初計画では静的アセット配信に採用予定だったが、構築しないことにした。**

| 当初の想定 | 実態 |
|------|--------|
| 用途：静的アセット配信（ゲームクライアント・アップデートファイル） | **配信対象が存在しない**。Unity クライアントは未実装で、ゲームサーバーは GameLift Build として S3 経由で配布される（CloudFront を通らない経路） |
| オリジン：S3 | 置くべきファイルが無い |

配信物が無い状態で CDN だけを建てても、動作確認できるのは「空の箱が空を返すこと」だけで、
インフラ品質の証明にならない。**必要になった時点で追加する**（オリジンが S3、
証明書は us-east-1、という構成自体は Route 53 / ACM の実装で得た知見がそのまま使える）。

VPC を不採用にしたのと同じ判断基準 — **使い所が無いものは作らない**。詳細は docs/qa-log.md §13。

### 2.7 Route 53

| 項目 | 設定値 |
|------|--------|
| ルーティングポリシー | レイテンシールーティング |
| ヘルスチェック | 30秒間隔 |
| フェイルオーバー | 自動（東京→バージニア） |

---

## 3. 開発言語・フレームワーク仕様

### 3.1 言語・責務の分担

| レイヤー | 言語 | 責務 |
|---------|------|------|
| Unityクライアント | C#（.NET） | UI・ゲームロジック・サーバー接続 |
| GameLiftゲームサーバー | Go | リアルタイム通信処理・プレイヤー同期 |
| Lambdaバックエンド | Go | マッチメイキング・DynamoDB操作 |

### 3.2 Go フレームワーク・ライブラリ

| 用途 | ライブラリ | 選定理由 |
|------|----------|---------|
| GameLiftサーバー | 標準ライブラリ + GameLift Server SDK for Go | 軽量・低レイテンシー優先 |
| WebSocket通信 | gorilla/websocket | Go最大手のWebSocketライブラリ |
| Lambda | aws-lambda-go（AWS公式） | AWS公式サポート・コールドスタート最速 |
| DynamoDB操作 | aws-sdk-go-v2 | AWS公式SDK v2（推奨版） |
| JSONシリアライズ | 標準ライブラリ（encoding/json） | 依存最小化 |

> HTTPフレームワーク（Gin・Echo等）を採用しない理由：GameLiftサーバーはUDP/TCPのゲームサーバーであり、LambdaはAWS公式ルーティングで完結するため、依存を減らしてコールドスタート時間・メモリ使用量を最適化する。

### 3.3 Goプロジェクト構成

```
server/
├── cmd/
│   ├── gameserver/
│   │   └── main.go             # GameLiftサーバーエントリーポイント
│   └── lambda/
│       ├── match_request/
│       │   └── main.go
│       ├── match_status/
│       │   └── main.go
│       ├── match_cancel/
│       │   └── main.go
│       └── match_history/
│           └── main.go
├── internal/
│   ├── game/
│   │   ├── session.go          # ゲームセッション管理
│   │   ├── player.go           # プレイヤー状態管理
│   │   └── sync.go             # 位置・状態同期ロジック
│   ├── matchmaking/
│   │   └── handler.go          # マッチメイキングロジック
│   └── db/
│       └── dynamodb.go         # DynamoDB操作
├── pkg/
│   └── protocol/
│       └── message.go          # 通信メッセージ定義（C#と共通仕様）
├── go.mod
└── go.sum
```

### 3.4 goroutine活用方針

```go
// プレイヤーごとにgoroutineを割り当てて並行処理
func (s *Session) handlePlayer(player *Player) {
    go func() {
        for {
            msg := player.receive()
            s.broadcast(msg)
        }
    }()
}
```

### 3.5 Terraformディレクトリ構成

> 以下は計画時の構成。実際の構成は 2 点変わっている（最新は CLAUDE.md「Terraform レイアウト」）。
> - **`environments/anywhere` を追加**：GameLift Local が Server SDK 5.x で廃止され、代替の
>   GameLift Anywhere が必要になったため（計画時には存在しなかった概念）。開発検証専用なので
>   本番同等の `dev` とは state を分離する。あわせて `modules/gamelift` は
>   `gamelift_anywhere`（Anywhere 用）と `gamelift_ec2`（本番同等の Managed EC2 用）に分割。
> - **`modules/vpc` は不採用**：§2.1 の注記を参照。

```
terraform/
├── environments/
│   ├── dev/
│   │   ├── main.tf
│   │   ├── variables.tf
│   │   └── terraform.tfvars
│   └── prod/
│       ├── main.tf
│       ├── variables.tf
│       └── terraform.tfvars
└── modules/
    ├── vpc/
    ├── gamelift/
    ├── cognito/
    ├── api_gateway/
    ├── lambda/
    ├── dynamodb/
    └── monitoring/
```

### 3.6 アピール観点

| 観点 | 内容 |
|------|------|
| Goを選ぶ理由 | 高並行処理・低レイテンシー・軽量バイナリがゲームサーバーに最適 |
| フレームワーク最小化 | 依存を減らしメモリ・コールドスタートを最適化する設計判断ができる |
| goroutine活用 | プレイヤーごとの並行処理をGoのネイティブ機能で自然に実現 |
| 言語分離の設計力 | C#とGoの責務を明確に分離し、それぞれの強みを活かした設計ができる |

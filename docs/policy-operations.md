# 監視・運用・セキュリティ方針

---

## 1. 監視・運用方針

### 1.1 CloudWatch ダッシュボード

| ウィジェット | メトリクス |
|------------|----------|
| アクティブセッション数 | GameLift: ActiveGameSessions |
| 接続プレイヤー数 | GameLift: CurrentPlayerSessions |
| インスタンス数 | GameLift: ActiveInstances |
| API レイテンシー | API Gateway: Latency |
| Lambda エラー率 | Lambda: Errors / Invocations |
| DynamoDB レイテンシー | DynamoDB: SuccessfulRequestLatency |

### 1.2 アラート設定

| アラート名 | 条件 | 通知先 |
|-----------|------|--------|
| セッション数過多 | ActiveGameSessions > 900 | SNS → メール |
| APIエラー率上昇 | 5XXErrorRate > 1% | SNS → メール |
| Lambdaエラー急増 | Errors > 10回/5分 | SNS → メール |
| インスタンス不足 | AvailableGameSessions < 10 | SNS → メール |

### 1.3 X-Ray トレーシング

- API Gateway → Lambda → DynamoDB の全リクエストをトレース
- エラー発生時の原因特定を迅速化

### 1.4 ログ管理

| ログ種別 | 保存先 | 保存期間 |
|---------|--------|---------|
| アプリケーションログ | CloudWatch Logs | 30日 |
| GameLiftサーバーログ | S3 | 90日 |
| アクセスログ | S3 | 90日 |

---

## 2. セキュリティ方針

### 2.1 IAM設計方針

- **最小権限の原則**を徹底
- サービスごとに個別のIAMロールを作成
- ルートアカウントは使用禁止、MFAを必須化

### 2.2 ネットワークセキュリティ

| リソース | 許可インバウンド |
|---------|--------------|
| GameLiftインスタンス | UDP 7777（クライアントのみ） |
| Lambdaからのアクセス | DynamoDB（VPCエンドポイント経由） |
| 踏み台EC2 | SSH 22（開発者IPのみ） |

### 2.3 データ保護

- DynamoDB：保存時暗号化（AWS管理キー）
- S3：SSE-S3暗号化
- 通信：TLS 1.2以上を強制

### 2.4 WAF設定

- API Gatewayにaws-managed-rules-common-rule-setを適用
- レートリミット：1IPあたり1,000リクエスト/5分

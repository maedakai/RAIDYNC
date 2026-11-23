# RAIDYNC 仕様書インデックス

**バージョン：** 1.3.0  
**作成日：** 2026-05-12  
**対象：** AAAタイトル開発会社 転職用ポートフォリオ

---

## ドキュメント一覧

### 概要・計画

| ファイル | 内容 |
|---------|------|
| [overview.md](overview.md) | プロジェクト概要・技術スタック・コスト試算 |
| [roadmap.md](roadmap.md) | 開発ロードマップ（Phase 0〜7） |

### 仕様

| ファイル | 内容 |
|---------|------|
| [spec-architecture.md](spec-architecture.md) | システムアーキテクチャ・AWSインフラ仕様・Goプロジェクト構成 |
| [spec-game.md](spec-game.md) | ゲーム仕様・通信仕様・マッチメイキング仕様・非機能要件 |

### 方針

| ファイル | 内容 |
|---------|------|
| [policy-development.md](policy-development.md) | ローカル開発環境・CI/CD・テスト戦略 |
| [policy-operations.md](policy-operations.md) | 監視・運用・セキュリティ |

---

## クイックリファレンス

### アーキテクチャ概要

```
Unity Client / GoテストCLI
  → Route 53（レイテンシールーティング）
  → API Gateway + Lambda（Go）（認証・マッチメイキング）
  → Amazon GameLift（UDPゲームセッション）
  → DynamoDB Global Tables（東京 ↔ バージニア）
```

### 目標値

| 指標 | 目標値 |
|------|--------|
| 同時接続セッション数 | 1,000セッション（4,000接続） |
| APIレスポンスタイム | 200ms以内（p95） |
| 東京レイテンシー | 50ms以内 |
| スケールアウト時間 | 3分以内 |
| 可用性 | 99.9%以上 |

### 開発フェーズ概要

| フェーズ | 内容 | 期間 |
|---------|------|------|
| Phase 0 | ローカル環境構築 | 1週間 |
| Phase 1 | Terraformインフラ基盤 | 2週間 |
| Phase 2 | バックエンド実装・テスト | 2週間 |
| Phase 3 | GameLift・クライアント接続 | 3週間 |
| Phase 4 | マッチメイキング・E2E | 2週間 |
| Phase 5 | マルチリージョン化 | 2週間 |
| Phase 6 | 監視・負荷テスト | 2週間 |
| Phase 7 | ドキュメント・公開 | 1〜2週間 |

# ゲーム・通信・マッチメイキング仕様

---

## 1. ゲーム仕様

### 1.1 ゲームジャンル

**4人Co-opリアルタイムアクション（協力型ハンティングアクション）**

### 1.2 基本ルール

- 最大4人のプレイヤーが協力してミッションをクリアする
- 各プレイヤーの位置・状態をリアルタイムで同期する
- セッション時間は最大20分

### 1.3 プレイヤー仕様

| 項目 | 内容 |
|------|------|
| 最大プレイヤー数 / セッション | 4人 |
| 同時接続セッション数（想定） | 最大1,000セッション（4,000接続） |
| 対応プラットフォーム | PC（Windows） |

### 1.4 ゲームループ

```
1. ログイン（Cognito認証）
2. マッチメイキングリクエスト
3. セッション確立（GameLift）
4. ゲームプレイ（リアルタイム同期）
5. 結果保存（DynamoDB）
6. ロビーへ戻る
```

---

## 2. Unityクライアント仕様

### 2.1 バージョン・設定

| 項目 | 設定値 |
|------|--------|
| Unityバージョン | 2022 LTS以上 |
| ターゲットプラットフォーム | Windows（x64） |
| レンダリングパイプライン | URP（Universal Render Pipeline） |

### 2.2 使用パッケージ

| パッケージ | 用途 |
|-----------|------|
| Amazon GameLift SDK for Unity | サーバー接続 |
| AWS SDK for .NET | Cognito認証 |
| Newtonsoft.Json | JSON処理 |

### 2.3 シーン構成

| シーン名 | 用途 |
|---------|------|
| TitleScene | タイトル・ログイン画面 |
| LobbyScene | マッチメイキング・ロビー |
| GameScene | ゲームプレイ本体 |
| ResultScene | 結果表示 |

### 2.4 クライアントの責務範囲

```
Unity（C#）が担当する処理
├── Cognito認証（ログイン・トークン管理）
├── マッチメイキングAPIへのHTTPリクエスト
├── GameLiftサーバーへのUDP接続
├── プレイヤー入力処理
├── 受信データのゲームオブジェクト反映
└── UI（ログイン・ロビー・ゲーム・リザルト）
```

---

## 3. 通信仕様

### 3.1 プロトコル

| 通信種別 | プロトコル | 用途 |
|---------|----------|------|
| API通信 | HTTPS | 認証・マッチメイキング |
| ゲームデータ | UDP（GameLift） | リアルタイム位置同期 |

### 3.2 同期データ仕様

```json
{
  "sessionId": "string",
  "timestamp": "number（UnixTime ms）",
  "players": [
    {
      "playerId": "string",
      "position": { "x": "float", "y": "float", "z": "float" },
      "rotation": { "x": "float", "y": "float", "z": "float" },
      "health": "int（0〜100）",
      "state": "string（idle / moving / attacking / dead）"
    }
  ]
}
```

### 3.3 送信頻度

| データ種別 | 送信頻度 |
|-----------|---------|
| プレイヤー位置・状態 | 20回/秒（50ms間隔） |
| ゲームイベント（攻撃・被弾等） | イベント発生時 |
| セッション状態 | 1回/秒 |

### 3.4 C#とGoの共通メッセージフォーマット

```json
{
  "type": "string（PLAYER_MOVE / PLAYER_ACTION / SESSION_STATE）",
  "sessionId": "string",
  "playerId": "string",
  "timestamp": "number（UnixTime ms）",
  "payload": {}
}
```

| messageType | 送信元 | 内容 |
|-------------|--------|------|
| PLAYER_MOVE | C# → Go | プレイヤー位置・回転 |
| PLAYER_ACTION | C# → Go | 攻撃・スキル等のアクション |
| SESSION_STATE | Go → C# | 全プレイヤーの最新状態（ブロードキャスト） |
| SESSION_END | Go → C# | セッション終了通知 |

### 3.5 レイテンシー目標

| リージョン | 目標値 |
|-----------|--------|
| 東京（国内） | 50ms以内 |
| バージニア（北米） | 150ms以内 |

---

## 4. マッチメイキング仕様

### 4.1 設計方針

co-op型ジャンルでありながら、AWSサーバーレス（API Gateway + Lambda + DynamoDB + GameLift）のインフラ実装を見せ場にするため、**チケットベースの非同期マッチング**を採用する。co-op らしさは以下の3要素で担保する。

| 要素 | 役割 |
|------|------|
| **questId（クエストID）** | 「何を遊ぶか」でマッチさせる co-op の基本軸 |
| **partyId（任意）** | フレンドと組むための経路。同 partyId は必ず同一セッションに集約 |
| **soloAllowed（任意）** | 他人を待たず即セッション化（MH のソロ相当） |

> **同期方式が採れない理由：** API Gateway REST の最大応答時間は 29 秒、Lambda 最大タイムアウトは 30 秒。最大待機 60 秒を単一の同期リクエストで実現することは不可能なため、非同期方式は必然。

| 観点 | 採用方針 |
|------|---------|
| マッチング契機 | クライアントが定期的に `GET /match/status` を叩いた際にサーバ側がマッチング処理を試行（専用ワーカー不要） |
| 同期方式 | 受付（request）は即時応答、マッチ成立判定は非同期 |
| グルーピング基準 | (1) partyId 一致を最優先 (2) 同一 questId 内で集約 (3) 同一 region 内 |
| 副次基準 | レイテンシー（同 questId に複数候補がいる場合） |

### 4.2 マッチメイキングフロー

```
[A] 受付フェーズ（同期、〜200ms）
  1. クライアント → POST /match/request
       body: { questId, soloAllowed?, partyId?, latencyHint? }
  2. Lambda がチケットを MatchmakingTickets に WAITING で発行
  3. ticketId を即時返却

[B] マッチングフェーズ（非同期、最大 60 秒）
  4. クライアントが GET /match/status/{ticketId} を 2 秒間隔でポーリング
  5. status Lambda は以下を行う:
       a. チケットを参照
       b. soloAllowed=true なら即セッション作成 → MATCHED に遷移
       c. それ以外は MatchmakingTickets を questId-status-index で検索し、
          同 questId の WAITING チケットを集める
       d. 集めた候補から partyId 一致を優先しつつ 2〜4 人で構成
       e. TransactWriteItems で対象チケットを WAITING → MATCHING に
          条件付き遷移（version 一致で原子的に確保）
       f. GameLift にゲームセッション作成 → 接続情報取得
       g. 全チケットを MATCHED + 接続情報で更新
  6. 状況に応じて status を返す:
       WAITING / MATCHING / MATCHED + connectionInfo / TIMEOUT / CANCELLED

[C] 接続フェーズ
  7. クライアントは MATCHED の接続情報で GameLift サーバーに UDP 接続
```

### 4.3 マッチメイキングルール

| 項目 | ルール |
|------|--------|
| 最大待機時間 | 60秒（経過後は最低 2 人で見切り発車、それも揃わなければ TIMEOUT） |
| 最低マッチ人数 | 4 人（待機 60 秒経過後は 2 人まで許容） |
| 最大マッチ人数 | 4 人 |
| ソロ即時開始 | `soloAllowed: true` で 1 人即セッション化 |
| パーティ優先 | `partyId` が一致するチケットは必ず同一セッションへ集約 |
| マッチング基準 | (1) partyId (2) questId (3) region (4) レイテンシー（同点時） |
| 1プレイヤー1チケット | 同 playerId で WAITING が既に存在する場合、新規発行せず既存 ticketId を返す（冪等） |
| ポーリング推奨間隔 | 2 秒（過剰ポーリングは API Gateway 側のレート制限で後段制御） |

### 4.4 APIエンドポイント

| メソッド | パス | 説明 |
|---------|------|------|
| POST | /match/request | マッチリクエスト（チケット発行） |
| GET | /match/status/{ticketId} | マッチ状態確認（兼マッチャー起動） |
| DELETE | /match/cancel/{ticketId} | マッチキャンセル（WAITING 時のみ） |
| GET | /player/{playerId}/history | マッチ履歴取得 |

#### 4.4.1 POST /match/request

リクエスト:

```json
{
  "questId": "string (必須)",
  "soloAllowed": "bool (任意, default: false)",
  "partyId": "string (任意, パーティ内で共通)",
  "latencyHint": "number (任意, ms)"
}
```

レスポンス (200):

```json
{
  "ticketId": "string",
  "status": "WAITING",
  "createdAt": "number (UnixTime ms)",
  "expiresAt": "number (UnixTime ms)"
}
```

#### 4.4.2 GET /match/status/{ticketId}

ステータス別レスポンス (200):

```json
// WAITING
{ "ticketId": "...", "status": "WAITING", "waitedSec": 12 }

// MATCHING
{ "ticketId": "...", "status": "MATCHING" }

// MATCHED
{
  "ticketId": "...",
  "status": "MATCHED",
  "sessionId": "...",
  "connectionInfo": {
    "ipAddress": "...",
    "port": 7777,
    "playerSessionId": "..."
  }
}

// TIMEOUT / CANCELLED
{ "ticketId": "...", "status": "TIMEOUT" }
```

#### 4.4.3 DELETE /match/cancel/{ticketId}

WAITING の場合のみ CANCELLED に遷移。MATCHING / MATCHED の場合は 409 Conflict を返す。

### 4.5 チケットステートマシン

```
        ┌─────────┐
        │ WAITING │
        └────┬────┘
   ┌─────────┼──────────┐
   │ (claim) │ (60s 経過) │ (DELETE)
   ▼         ▼          ▼
┌──────────┐ ┌─────────┐ ┌───────────┐
│ MATCHING │ │ TIMEOUT │ │ CANCELLED │
└────┬─────┘ └─────────┘ └───────────┘
     │ (session created)
     ▼
┌─────────┐
│ MATCHED │
└─────────┘
```

### 4.6 並行制御

複数の `match_status` Lambda が同じ WAITING チケットを取り合うのを防ぐため:

- 各チケットに `version: number` を持たせる
- 状態遷移は **TransactWriteItems + ConditionExpression (`#status = :waiting AND #version = :v`)** で行う
- 失敗側は「他のマッチャーが既に組んだ」とみなして次の試行へ進む（リトライ不要）

### 4.7 MatchmakingTickets テーブル

| 項目 | 設定値 |
|------|--------|
| PK | ticketId（String） |
| 主な属性 | playerId, questId, partyId, status, version, region, createdAt, expiresAt, connectionInfo, ttl |
| GSI: questId-status-index | (questId HASH, status RANGE) — マッチャーが同 quest の WAITING を抽出 |
| GSI: playerId-status-index | (playerId HASH, status RANGE) — 1プレイヤー1チケット制約の検証 |
| TTL 属性 | `ttl`（UnixTime 秒）— 待機タイムアウト + 余裕で 90 秒程度 |
| Global Tables | **不要**（region-local の短命データ） |

### 4.8 設計上の割り切り（Phase 2-4 スコープ）

| 項目 | 扱い |
|------|------|
| Backfill（ゲーム中の欠員補充） | スコープ外。Phase 6 以降の将来拡張 |
| スキルレーティング | スコープ外。questId と region のみで分類 |
| クエストカタログ | 固定値で 5 種類程度 (`quest-001`〜`quest-005`) を想定 |
| MatchmakingTickets の Global Tables | 不要（region-local の短命データ） |
| regionPref パラメータ | 採用しない（Route 53 レイテンシールーティングと二重になるため） |
| マッチング契機 | status ポーリング駆動。EventBridge スケジューラ等は使わない |

---

## 5. 非機能要件

### 5.1 パフォーマンス目標

| 指標 | 目標値 |
|------|--------|
| 同時接続セッション数 | 最大1,000セッション |
| 東京リージョン レイテンシー | 50ms以内 |
| スケールアウト時間 | 3分以内 |
| 環境再構築時間（IaC） | 15分以内 |
| API レスポンスタイム | 200ms以内（p95） |

### 5.2 可用性目標

| 指標 | 目標値 |
|------|--------|
| サービス可用性 | 99.9%以上 |
| RTO（目標復旧時間） | 15分以内 |
| RPO（目標復旧時点） | 1時間以内 |

### 5.3 スケーラビリティ

- GameLiftオートスケーリングによる水平スケールアウト
- DynamoDB Global Tablesによるグローバルレプリケーション
- Route 53レイテンシールーティングによる自動リージョン切り替え

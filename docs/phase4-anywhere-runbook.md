# Phase 4 GameLift Anywhere 実接続 手順（Runbook）

自 PC を GameLift Anywhere フリートの **compute** として登録し、`cmd/gameserver`（GameLift Server SDK 5.x 連携）が **実 GameLift の制御プレーンと会話できるか**を検証する手順。Windows / PowerShell 前提。

> **目的の再確認**：ここで検証するのは **GameLift Server SDK 契約**（`InitSDK→ProcessReady→OnStartGameSession→…→ProcessEnding`）。Managed EC2 の **デプロイ経路（Build/Fleet/EC2 プロビジョニング）は対象外**（それは AWS dev E2E の担当）。詳細は docs/qa-log.md §11。
>
> **方針**：まず AWS CLI で手動疎通し、動いた構成を **後で Terraform 化**する（CLAUDE.md「IaC 徹底」への戻しは疎通後の必須タスク）。

構成:

```
ゲームサーバ実行 = 自 PC（cmd/gameserver, UDP :7777）
        ↑↓ GameLift Server SDK（WebSocket, AuthToken 認証）
制御プレーン     = 実 AWS の GameLift（Anywhere フリート / custom location）
セッション起動   = aws gamelift create-game-session（GameProperties で questId/playerIds を注入）
```

---

## 前提

- **AWS アカウント**と、`gamelift:*` を実行できる IAM 権限（CLI プロファイル）。
- **AWS CLI v2** が設定済み（`aws sts get-caller-identity` が通る）。
- リージョンは `ap-northeast-1`（プロジェクト既定）を使用。
- Go でビルド可能（`go build ./cmd/gameserver`）。
- UDP :7777 が空いていること。

> **コスト**：Anywhere フリートは **EC2 を持たない**ため instance-hour 課金は発生しない（≈無料枠）。ゲームセッション実行中の GameLift 課金は極小。ただしフリート/ロケーションは作りっぱなしにせず、検証後は削除する（末尾「後片付け」）。

---

## 環境変数（この Runbook で使う値）

以下は各ステップの出力で埋めていく。PowerShell 変数として持ち回る想定:

| 変数 | 由来 | 例 |
|---|---|---|
| `$FLEET_ID` | ステップ2 create-fleet | `fleet-xxxxxxxx-...` |
| `$LOCATION` | ステップ1 で決める（`custom-` 始まり必須） | `custom-raidync-local` |
| `$COMPUTE` | 任意に決める compute 名 | `raidync-local-pc` |
| `$WEBSOCKET_URL` | ステップ3 register-compute の `GameLiftServiceSdkEndpoint` | `wss://ap-northeast-1.api.amazongamelift.com/...` |
| `$AUTH_TOKEN` | ステップ4 get-compute-auth-token の `AuthToken` | `eyJ...`（短命・失効あり） |

---

## ステップ1: カスタムロケーション作成

Anywhere フリートは AWS 管理の AZ ではなく **自前ロケーション**にひも付く。名前は **`custom-` で始める**制約がある。

```powershell
$LOCATION = "custom-raidync-local"
aws gamelift create-location `
  --location-name $LOCATION `
  --region ap-northeast-1
```

冪等でないため既存なら `ConflictException` になる（その場合はこのまま次へ）。

---

## ステップ2: Anywhere フリート作成

```powershell
aws gamelift create-fleet `
  --name "raidync-anywhere" `
  --compute-type ANYWHERE `
  --locations "Location=$LOCATION" `
  --region ap-northeast-1
```

出力の `FleetAttributes.FleetId` を控える:

```powershell
$FLEET_ID = "fleet-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"   # ← 上の出力から
```

`Status` が `ACTIVE` になるまで数十秒待つ（Anywhere は EC2 プロビジョニングが無いので早い）:

```powershell
aws gamelift describe-fleet-attributes --fleet-id $FLEET_ID --region ap-northeast-1 `
  --query "FleetAttributes[0].Status"
```

---

## ステップ3: 自 PC を compute として登録

```powershell
$COMPUTE = "raidync-local-pc"
aws gamelift register-compute `
  --compute-name $COMPUTE `
  --fleet-id $FLEET_ID `
  --ip-address 127.0.0.1 `
  --location $LOCATION `
  --region ap-northeast-1
```

出力 `Compute.GameLiftServiceSdkEndpoint` が **SDK が接続する WebSocket URL**:

```powershell
$WEBSOCKET_URL = "wss://ap-northeast-1.api.amazongamelift.com/..."   # ← 上の出力から
```

> `--ip-address` はプレイヤーが接続する IP。ローカル検証なので `127.0.0.1`。Unity クライアントも同一 PC 前提。

---

## ステップ4: compute 用 auth token を取得

WebSocket 接続の認証トークン（短命）。**失効したら取り直す**。

```powershell
aws gamelift get-compute-auth-token `
  --fleet-id $FLEET_ID `
  --compute-name $COMPUTE `
  --region ap-northeast-1

$AUTH_TOKEN = "eyJ..."   # ← 出力 AuthToken から
```

---

## ステップ5: gameserver を Anywhere モードで起動（ターミナルA）

SDK は `InitSDKFromEnvironment()` で以下の環境変数を読む（コード実測: `common/consts.go`）。`AuthToken` があれば認証方式はそれが優先される。

```powershell
cd C:\work\RAIDYNC\server

# --- GameLift Server SDK 用（InitSDKFromEnvironment が読む）---
$env:GAMELIFT_SDK_WEBSOCKET_URL = $WEBSOCKET_URL
$env:GAMELIFT_SDK_AUTH_TOKEN    = $AUTH_TOKEN
$env:GAMELIFT_SDK_FLEET_ID      = $FLEET_ID
$env:GAMELIFT_SDK_HOST_ID       = $COMPUTE          # compute 名 = HostID
$env:GAMELIFT_SDK_PROCESS_ID    = "raidync-proc-1"  # プロセス識別子（任意・ユニーク）

# --- 本アプリの起動フラグ ---
$env:GAMELIFT_ENABLED    = "true"      # bridge.Activate() を有効化
$env:GAMESERVER_BIND     = ":7777"

go run .\cmd\gameserver
```

期待ログ（抜粋）:

```
gameserver listening on udp [::]:7777 ...
GameLift mode: ready, waiting for game sessions      ← ProcessReady 成功
```

ここまで出れば **SDK が GameLift 制御プレーンに ProcessReady を通知できた**＝Anywhere 疎通の最初の山を越えた。

> AppControl(WDAC) でビルド済み exe が弾かれる場合は上記のように `go run` で回避（過去の実績どおり）。

---

## ステップ6: ゲームセッションを起動して OnStartGameSession を発火（ターミナルB）

本番ではマッチ成立時に Lambda が `CreateGameSession` を呼ぶ（G2・未結線）。今回は **手動で create-game-session** して SDK コールバック経路だけを検証する。

`parseSessionConfig`（`internal/gameserver/gamelift.go`）は GameProperties の **`questId`** と **`playerIds`（カンマ区切り）** を読む。合わせて渡す:

```powershell
aws gamelift create-game-session `
  --fleet-id $FLEET_ID `
  --location $LOCATION `
  --maximum-player-session-count 4 `
  --game-properties "Key=questId,Value=quest-rathalos" "Key=playerIds,Value=p1,p2" `
  --region ap-northeast-1
```

ターミナルA（gameserver）に以下が出れば **OnStartGameSession が発火・roster 登録成功**:

```
game session started: <gameSessionId> (quest=quest-rathalos roster=[p1 p2])
```

これで **「マッチ成立 → GameLift がセッション配置 → サーバの OnStartGameSession」** の本物の経路が通ったことになる（配置指示だけ手動）。

---

## ここまでの達成と残り（フォローアップ）

- ✅ **達成**：InitSDK / ProcessReady / OnStartGameSession / ヘルスチェックの **SDK 契約が実 GameLift で動作**。
- ⏭ **残 G1**：プレイヤー個別の `AcceptPlayerSession(playerSessionId)` / `RemovePlayerSession` は未実装。UDP client からの実接続検証（⑤の client 疎通）と合わせて次ステップで対応。
- ⏭ **残 G2**：`matchmaking` の `GameLiftStub` を実 `CreateGameSession` に差し替え（Lambda→GameLift 自動配置）。
- ⏭ **Terraform 化**：動いた location/fleet を `terraform/modules/gamelift` + `environments/dev` に落とす（IaC 徹底への戻し）。

---

## 後片付け（重要）

フリート/ロケーションを残さない。ゲームセッションは自動終了するが、フリートは明示削除:

```powershell
aws gamelift delete-fleet --fleet-id $FLEET_ID --region ap-northeast-1
# ロケーションは他フリートが使っていなければ削除可
aws gamelift delete-location --location-name $LOCATION --region ap-northeast-1
```

gameserver は ターミナルA で Ctrl+C（`OnProcessTerminate`→`ProcessEnding` 経路）。

---

## トラブルシューティング

- **`InitSDKFromEnvironment` で失敗**：`GAMELIFT_SDK_WEBSOCKET_URL` / `GAMELIFT_SDK_AUTH_TOKEN` が空、または AuthToken 失効。ステップ4 を再実行してトークンを取り直す。
- **`ProcessReady` 後に接続が切れる**：ヘルスチェック未応答 or WebSocket 認証切れ。`onHealthCheck` は常に true を返す実装なので、多くは AuthToken 失効。
- **create-game-session が `FleetCapacityExceededException`**：Anywhere は compute 1 台につき同時プロセス数に上限。既存セッションを終わらせるか別 process-id で起動。
- **OnStartGameSession が発火しない**：`--location` がフリート登録の `$LOCATION` と一致しているか、compute が `--location` 配下に register されているか確認。
- **roster が空**：`--game-properties` の Key が `questId` / `playerIds` になっているか（大文字小文字厳密）。`playerIds` は `Value=p1,p2` のようにカンマ区切り。

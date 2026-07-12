# Phase 4 ローカル E2E 実行手順（Runbook）

`cmd/client` による端から端までの動作確認手順。Windows / PowerShell 前提。

構成（すべてローカル・AWS 不使用）:

```
client ─HTTP→ mock-auth(:9229)        … signup / login（JWT 発行）
client ─HTTP→ localapi(:8080)         … match/request・status・cancel・history・session/end
client ─UDP →  localapi 同居 gameserver(:7777)  … PLAYER_MOVE / SESSION_STATE
localapi ─→ LocalStack DynamoDB(:4566)        … Tickets / Sessions / MatchHistory
```

E2E フロー: `login → match/request → poll status → UDP play → session/end → history`。

---

## 前提

- Docker Desktop が起動していること
- Go がインストール済み（`go version`）
- ポート 4566 / 9229 / 8080 / 7777 が空いていること

---

## ステップ1: Docker スタック起動（LocalStack + mock-auth）

DynamoDB テーブルは LocalStack 起動時に `ready.d` の init スクリプトが**自動作成**する（手動の create は不要）。

```powershell
cd C:\work\RAIDYNC
docker compose -f docker/docker-compose.yml up -d
```

確認（数十秒待ってから）:

```powershell
# LocalStack 健康確認
curl http://localhost:4566/_localstack/health
# mock-auth 健康確認 → {"status":"ok"}
curl http://localhost:9229/health
# テーブルが 4 つ出来ているか（コンテナ内の awslocal を利用）
docker exec raidync-localstack awslocal dynamodb list-tables --region ap-northeast-1
```

`TableNames` に `Players` / `Sessions` / `MatchHistory` / `MatchmakingTickets` が出れば OK。

---

## ステップ2: バイナリをビルド

```powershell
cd C:\work\RAIDYNC\server
go build -o ..\bin\localapi.exe .\cmd\localapi
go build -o ..\bin\client.exe   .\cmd\client
```

---

## ステップ3: localapi を起動（ターミナルA）

このターミナルは起動しっぱなしにする。環境変数をセットしてから実行:

```powershell
cd C:\work\RAIDYNC
$env:DYNAMODB_ENDPOINT     = "http://localhost:4566"
$env:AWS_REGION            = "ap-northeast-1"
$env:AWS_ACCESS_KEY_ID     = "test"      # LocalStack 用ダミー
$env:AWS_SECRET_ACCESS_KEY = "test"
$env:AUTH_JWKS_URL         = "http://localhost:9229/.well-known/jwks.json"
$env:AUTH_ISSUER           = "http://localhost:9229"
$env:AUTH_CLIENT_ID        = "raidync-local-client"
.\bin\localapi.exe
```

期待ログ（抜粋）:

```
localapi: gameserver(udp) listening on 127.0.0.1:7777 (advertise 127.0.0.1:7777)
localapi: listening on :8080
```

---

## ステップ4: client を実行（ターミナルB・別ウィンドウ）

```powershell
cd C:\work\RAIDYNC
.\bin\client.exe
```

主要フラグ（必要なら）:

| フラグ | 既定 | 説明 |
|---|---|---|
| `-email` / `-password` | `player1@example.com` / `password123` | ログイン資格情報 |
| `-quest` | `quest-rathalos` | マッチ対象クエスト |
| `-solo` | `true` | 単独で即マッチ成立させる |
| `-moves` | `10` | 送る PLAYER_MOVE 数 |
| `-result` | `CLEAR` | 報告する結末（CLEAR/FAILED/ABANDONED） |

期待ログ（抜粋・順序）:

```
client: authenticated playerId=... token=...
client: match requested ticketId=tkt-...
client: matched sessionId=sess-... connect=127.0.0.1:7777
client: udp connected to 127.0.0.1:7777
client: <- SESSION_STATE seq=1 players=1 self.health=100
...
client: play done sent=10 moves +1 action, received=N SESSION_STATE
client: result reported status=ENDED playersSaved=1 result=CLEAR
client: history entries=1
client:   [0] session=sess-... quest=quest-rathalos result=CLEAR duration=...s
client: E2E completed
```

**`client: E2E completed`** が出れば成功（report→history が DynamoDB を介してつながったことの確証）。

---

## 後片付け

```powershell
# localapi は ターミナルA で Ctrl+C
cd C:\work\RAIDYNC
docker compose -f docker/docker-compose.yml down
```

---

## トラブルシューティング

- **`localapi.exe` 実行が Windows AppControl(WDAC) でブロックされる**
  暗号/HTTP を含むバイナリは弾かれることがある。まず `go run .\cmd\localapi`（server ディレクトリから、上の環境変数を設定した上で）を試す。それも弾かれる場合はこの手順の結果（ブロックされた旨）を共有してほしい。

- **`no SESSION_STATE received` で失敗**
  UDP の roster 不一致の可能性。client の playerId（JWT sub）と、マッチ時にゲームサーバへ登録された roster がずれていないか localapi 側ログを確認。

- **`match not formed: status=TIMEOUT`**
  `-solo true`（既定）で実行しているか確認。false だと 4 人揃うまで成立しない。

- **`unexpected status 401`（match/request 時）**
  `AUTH_ISSUER` が mock-auth の発行 iss（`http://localhost:9229`）と一致しているか、`AUTH_JWKS_URL` が到達可能かを確認。

- **ポート使用中**
  既存プロセスを止めるか、localapi は `$env:LOCALAPI_ADDR` / `$env:GAMESERVER_BIND_ADDR`、client は `-api` で変更可能。

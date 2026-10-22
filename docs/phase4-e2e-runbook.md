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
$env:OTEL_EXPORTER_OTLP_INSECURE = "true"   # ローカルの Jaeger は平文で待ち受ける（OTLP gRPC は既定で TLS）
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
client: matched sessionId=sess-... connect=127.0.0.1:7777 psid=psess-...
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

> **psid 配管の確認（G1-full）**：client は match/status で受け取った `psid=psess-...` を UDP 毎パケットに提示する。ターミナルA（localapi）側には参加/離脱時に次のログが出る（`loggingObserver` が実 AWS の `GameLiftBridge` の代役で、`AcceptPlayerSession`/`RemovePlayerSession` 呼び出しに相当）:
> ```
> gameserver: player join session=sess-... player=... psid=psess-... (local: AcceptPlayerSession 相当)
> gameserver: player leave session=sess-... player=... psid=psess-... (local: RemovePlayerSession 相当)
> ```
> 実 GameLift での psid 承認確認は AWS dev E2E の担当（docs/phase4-anywhere-runbook.md 末尾フォローアップ参照）。

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

---

# AWS dev E2E：フリート削除手順（重要）

> ここから下は**ローカル E2E ではなく、`terraform apply` した AWS dev 環境**の後片付け。
> GameLift Managed EC2 フリートは **ACTIVE な間ずっとインスタンス時間課金**される唯一のリソース（プレイヤー 0 でも課金）。
> E2E が終わったら**必ず**削除すること。他リソース（DynamoDB/Lambda/API GW/Cognito/S3）は実質従量課金なので、最優先で止めるべきはフリート。
>
> リージョンは `ap-northeast-1`、対象は `raidync-dev-fleet`（S3 バケットは `raidync-dev-gamelift-build-444952542001`）。

## 方法A：Terraform で環境ごと destroy（推奨）

構成全体を IaC の逆順で畳む。フリート単体でなく apply したもの全部を消すのが基本。

```powershell
cd C:\work\RAIDYNC
terraform -chdir=terraform/environments/dev destroy -var-file=dev.tfvars
```

注意:
- **フリート削除は数分〜15分かかる**（`DELETING` → インスタンス終了で課金停止）。**途中で Ctrl+C しない**（中断するとフリートが残り課金が続く）。
- apply 直後でフリートが `ACTIVATING` 中だと `DeleteFleet` が弾かれることがある。**`ACTIVE` になってから** destroy する。
- 途中でエラーになったら**同じ destroy を再実行**すれば残りだけ消える（state ベースで冪等）。
- state 分離のため、この destroy は `anywhere` 環境には触れない。

## 方法B：AWS コンソール（GUI）でフリートだけ削除

Terraform を使わず GUI で今回のフリートを手動削除する場合。**IaC 状態と乖離する**ので、原則は方法A。GUI 操作は `raidync-admin`（MFA 付き）でログインして行う。

1. リージョンを **東京（ap-northeast-1）** に切り替える（画面右上）。
2. **Amazon GameLift** コンソール → 左メニュー **Hosting → Fleets**。
3. `raidync-dev-fleet` を選択 → **Actions → Delete**。
4. 確認ダイアログでフリート名を入力 → **Delete**。ステータスが **DELETING** に変わる。
5. 数分待ち、一覧から消える（＝インスタンス終了・課金停止）まで確認する。
6. （任意）**Builds** から `raidync-dev-gameserver` も削除。
7. **S3** コンソール → `raidync-dev-gamelift-build-444952542001` を空にして削除（Build zip が残っていると微少だが課金対象）。

> GUI でフリートだけ消すと Terraform state には「まだ在る」と記録が残る。次に方法Aを使うときは `terraform destroy` が「既に無い」を検知して整合するが、混乱を避けるため**GUI 削除後は state を捨てる（未 apply からやり直す）か、`terraform refresh` で同期する**こと。

## 方法C：AWS CLI でフリートだけ削除（緊急/フォールバック）

state が壊れて方法Aが使えない、または課金を今すぐ止めたいときの最短手段。

```powershell
# 1. 今回のフリート ID を確認
aws gamelift describe-fleet-attributes --region ap-northeast-1 `
  --query "FleetAttributes[?Name=='raidync-dev-fleet'].[FleetId,Status]" --output table

# 2. フリート削除（課金源を止める）
aws gamelift delete-fleet --fleet-id <上で得た FleetId> --region ap-northeast-1

# 3. Build 配布バケットを中身ごと削除
aws s3 rb s3://raidync-dev-gamelift-build-444952542001 --force
```

## 削除後の独立検証（必須）

「完了」表示を信じるだけでなく、課金源が本当に消えたかを直接確認する。

```powershell
# フリートが消えたか（該当なし が正常）
aws gamelift list-fleets --region ap-northeast-1

# Build 配布バケットが消えたか（出力が空 が正常）
aws s3 ls | Select-String raidync-dev-gamelift-build
```

どちらも空なら課金源は停止。残っていたら方法C で個別に削除する。

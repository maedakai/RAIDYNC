# RAIDYNC 質問ログ

> プロジェクト（設計・実装・インフラ・運用）についての質問と回答の要点を、フェーズを問わず蓄積する学習ログ兼ポートフォリオ素材。
> C#/Unity 背景の視点での疑問が中心。詳細な実装は各ソース・`docs/specification.md` を参照。
> 以下は Phase 0〜2 のぶん。新しい質問は適切なセクションへ追記する。

## 目次
1. [Go プロジェクト構造](#1-go-プロジェクト構造)
2. [マッチメイキングの設計判断](#2-マッチメイキングの設計判断)
3. [DynamoDB](#3-dynamodb)
4. [マッチングロジック](#4-マッチングロジック)
5. [JWT 認証](#5-jwt-認証)
6. [テスト（単体・結合）](#6-テスト単体結合)
7. [意図的な割り切り](#7-意図的な割り切り)
8. [テストの実機確認](#8-テストの実機確認)
9. [Git / SSH 環境構築](#9-git--ssh-環境構築)
10. [Phase 3：ゲームサーバー / トランスポート](#10-phase-3ゲームサーバー--トランスポート)
11. [Phase 4：ローカル E2E ハーネス](#11-phase-4ローカル-e2e-ハーネス)

---

## 1. Go プロジェクト構造

### Q. Lambda で Go を使うとき、`main.go` を 1 つの Lambda 機能ごとに配置する認識でいい?
A. はい。`cmd/lambda/<機能名>/main.go` という構成で、**1 機能 = 1 `main` パッケージ = 1 ビルド成果物**。`match_request` / `match_status` / `match_cancel` / `match_history` / `authorizer` がそれぞれ独立した Lambda になる。

### Q. `cmd` / `internal` / `pkg` のディレクトリ構成の理由は?
A. Go コミュニティの標準レイアウト。
- **`cmd/`** … 実行可能エントリポイント（`main` パッケージ）。
- **`internal/`** … 非公開実装。**コンパイラがモジュール外からの import を強制的に禁止**する特別なディレクトリ。
- **`pkg/`** … 外部に公開してよい共有型・ユーティリティ（例 `pkg/protocol/types.go`）。

### Q. `internal` 内のファイルは `server` からは参照できるが `RAIDYNC` からは参照できない?
A. その通り。`internal` は「`internal` の親ディレクトリをルートとするツリー内からのみ import 可能」というルール。`server/internal/...` は `server/` 配下からは使えるが、`server/` の外（別モジュール）からは使えない。アクセス修飾子の `internal`（C#）に近いが、**ディレクトリ位置で機械的に決まる**点が違う。

### Q. import が `github.com/raidync/server/*` とローカル内容なのに URL 形式なのはなぜ?
A. `go.mod` の `module` 宣言で決めた**モジュールパス**が import の接頭辞になるため。実際に GitHub へ取りに行くわけではなく、**一意な名前空間**として URL 形式の慣習を使っているだけ。ローカルのコードでも宣言したパスで参照する。

### Q. この接頭子は `go.mod` につき 1 つだけ?
A. はい。`module` 宣言は 1 つの `go.mod` に 1 つ。このリポジトリは `server`（メイン）と `docker/mock-auth` が**別々の `go.mod` を持つマルチモジュール構成**で、それぞれ独自の接頭辞を持つ。

### Q. `go.mod` は C# の `.csproj` に近い?
A. 近い。依存パッケージとビルド対象を定義する点は同じ。ただし `go.mod` は**モジュール単位**（複数パッケージを束ねる）で、`.csproj`（プロジェクト=アセンブリ単位）より粒度が大きい。

### Q. API Gateway の内容が `lambda` ディレクトリ内にあるのは意図的?
A. 意図的。`internal/apigw/` に置いているのは「API Gateway ⇄ Lambda 間の**変換ヘルパー**」（レスポンス整形 `respond.go`、認証情報の取り出し `auth.go`）。API Gateway 本体ではなく、Lambda 側で必要になる薄い接続層。

### Q. API Gateway はローカルに存在しないから自作している認識でいい?
A. おおむね正しい。LocalStack が API Gateway をエミュレートするが、認証情報の受け渡し（Authorizer Context）など**ローカルと AWS で挙動が揃わない部分**を吸収するために薄いヘルパーを自作している。

### Q. 現状実装している各ファイル（パッケージ）の相関関係は?
A. 実装は **2 つの平面**に分かれ、その間を **`pkg/protocol`（共有契約）**が橋渡しする。`protocol` は何にも依存せず全層から依存される中心（C# のドメインモデル/DTO 相当）。

```
                  pkg/protocol（共有契約・依存ゼロの中心）
       REST側: Ticket/Session/ConnectionInfo   RT側: GameMessage/PlayerState
                  ▲                                   ▲
 ── REST平面（マッチメイキング: Lambda/HTTPS）──   ── RT平面（ゲーム: UDP）──
 cmd/lambda/authorizer → internal/auth(Verifier)  cmd/gameserver/main（薄い入口）
 cmd/lambda/match_{request,status,cancel,history}   → internal/gameserver/server（UDP I/O+ループ）
   → internal/apigw（PlayerID/JSON応答）               → internal/game（Hub/Session/player）
   → internal/matchmaking（Matcher/selectGroup）     → internal/gameserver/gamelift（SDK連携）
       → SessionProvider（gamelift_stub）┄┄┄┄┄┄┄┄┄► （Phase3で実GameLiftへ）
   → internal/db（Client）→ DynamoDB
```

- **REST 平面（Phase 2）**: `authorizer` が JWT 検証→`sub` を Authorizer Context へ。業務 Lambda は `apigw.PlayerID()` で読む。`matchmaking.Matcher` が `Store` IF 越しに DynamoDB を操作してチケットを組成。`internal/db` だけが DynamoDB に触れる末端。
- **RT 平面（Phase 3）**: `cmd/gameserver` は配線のみ。`gameserver/server.go` が UDP I/O と 50ms tick、`internal/game` が状態更新とスナップショット生成（送信は `Sender` IF で抽象化し net 依存を隔離）。`gamelift.go` が SDK ライフサイクルを Hub に橋渡し。
- **2 平面の継ぎ目（最重要）**: マッチ結果 `matchmaking.finalize → protocol.Session.PlayerIDs` が、GameLift の `GameProperties["playerIds"]` → `gamelift.go parseSessionConfig` → `game.NewSession(roster)` へ流れる。**`Session.PlayerIDs`（REST）＝ `Session.roster`（RT）**を一致させることで、別プロセス・別プロトコルの 2 平面が論理結合する。現状この継ぎ目は `gamelift_stub`（固定接続先）で、Phase 3 後半に実 GameLift へ差し替え。
- **依存方向の規律**: 全層が内側の `protocol` に依存し `protocol` は何にも依存しない。`db`/`game` は末端。`net` は `gameserver` 層、AWS SDK は `db`/`gamelift` 層に局所化。`Store`/`SessionProvider`/`Sender`/`apigw.PlayerID` の各 IF が簡略版→本番版の無痛差し替えを可能にする（処理フロー視点は §10「Phase 3 各ファイルのシーケンス」を参照）。

---

## 2. マッチメイキングの設計判断

### Q. タイムアウトとポーリングの関係は?
A. API Gateway は 29 秒、Lambda は最大 30 秒で打ち切られる一方、マッチ待機は最大 60 秒必要。そのため**同期で待ち続けず**、チケットを発行して即返し、クライアントが数秒間隔（`PollIntervalSec=2`）で `match_status` をポーリングする**非同期チケット方式**にしている。

### Q. チケットの各要素（questId / partyId / soloAllowed）の意味は?
A. **同一系統のプレイヤーをグルーピングする**ための分類・制約。
- `questId` … 同じクエストを希望する者だけを集める**フィルタ**。
- `partyId` … 既存パーティを**絶対に分割しない**ための束縛キー。
- `soloAllowed` … ソロ希望者を他とまとめないためのフラグ。

### Q. 楽観ロックで status(WAITING→MATCHING) と version(0→1) は同時に更新される?
A. はい、**同一の更新操作（1 トランザクション）内で同時に**変わる。`ConditionExpression` で「現在 status=WAITING かつ version=現在値」を条件にし、満たせば status と version を**まとめて**書き換える。条件が満たせなければ全体が失敗する。

### Q. B,C,D 取り合いシナリオ（複数の matcher が同じ待機者を奪い合う）は解決していない?
A. 解決している。`TransactWriteItems` の **All-or-Nothing** 性質により、先に確保した側だけが成功し、競合した側は `ErrConflict` で**全件ロールバック**される。中途半端に一部だけ MATCHING になることはない。

### Q. DynamoDB への処理は直列なので、後続のマッチングはすべてロールバックされる認識でいい?
A. はい。トランザクションは原子的。条件不一致が 1 件でもあれば、そのトランザクション内の全 Put/Update が取り消され、チケットは WAITING のまま保たれる。次のポーリングで再度マッチングが試みられる。

### Q. マッチング成立処理をクライアントに任せることでインフラ構成が簡略化されている認識でいい?
A. 正しい。専用のワーカーや EventBridge スケジューラを置かず、**クライアントのポーリングが matcher の実行契機**になる。チケットは受動的なデータ、Lambda が能動的なアクター、という役割分担でインフラを軽くしている。

---

## 3. DynamoDB

### Q. PK / SK / GSI / TTL / Global Tables は何を指し、何をしている?
A.
- **PK（パーティションキー）** … レコードを一意に引くための主キー。データの物理的な配置先も決める。
- **SK（ソートキー）** … 同一 PK 内で複数レコードを並べ・範囲検索するためのキー（例 MatchHistory は playerId(PK)+timestamp(SK)）。
- **GSI（グローバルセカンダリインデックス）** … PK 以外の属性で検索するための「別の引き口」。本プロジェクトでは `questId-status-index` / `playerId-status-index`。**結果整合**である点に注意。
- **TTL** … 指定した時刻（epoch 秒）を過ぎたレコードを DynamoDB が自動削除。MatchmakingTickets は 90 秒で消える短命データ。
- **Global Tables** … 複数リージョン間で自動レプリケーションするマルチマスター構成。Players/Sessions/MatchHistory は対象、短命で region-local な MatchmakingTickets は対象外。

---

## 4. マッチングロジック

### Q. A が B,C,D でセッションを作るのは分かるが、B,C,D も同時にセッションを作ろうとしない? E のステータスは既に MATCHING では?
A. 作ろうとしない。`ClaimTickets` でグループ全員のチケットを**一括で WAITING→MATCHING に確定**させた「確保した matcher」だけが、その後セッション生成（finalize）まで責任を持つ。他のチケット（B,C,D）は既に MATCHING なので、それぞれのポーリングでは「成立済み」を受け取るだけで、新たにセッションを作らない（パススルー）。E も確保済みなら同様。

### Q. ③（セッション生成）より先に④を置かないのはなぜ?
A. **チケットの確保（MATCHING 確定）を先に済ませてからセッションを生成する**順序にしている。先にセッションだけ作って確保に失敗すると、**使われない幽霊セッション**が残ってしまう。確保成功を確認してから外部リソース（GameLift セッション）を作ることで、整合性を保ち無駄を防ぐ。

---

## 5. JWT 認証

要点（mock-auth ⇄ authorizer ⇄ 業務 Lambda の流れ）:
- mock-auth が RS256 で署名した JWT を発行し、`/.well-known/jwks.json` で公開鍵（JWKS）を配布。
- API Gateway の **TOKEN 型カスタムオーソライザ**（`authorizer` Lambda）が `Authorization: Bearer <jwt>` を検証。
- 検証成功時、`sub`（=playerId）を **Authorizer Context** に載せて業務 Lambda へ渡す。
- 業務 Lambda は `apigw.PlayerID()` 経由で playerId を取得。
- 検証は署名・発行者(iss)・対象(aud/client_id)・有効期限を確認。`sub` 欠落や改ざんは拒否。

### Q. authorizer Lambda は他の Lambda の API を叩かれたときに使われる? 単独稼働はある?
A. 主語を 1 つ補正すると正確になる。
- **呼ぶのは API Gateway**: 保護ルートにリクエストが来ると、API Gateway が **authorizer → 業務 Lambda の順**で呼ぶ。「業務 Lambda が authorizer を呼ぶ」のではない。authorizer はクライアントから直接叩けず、業務 Lambda とも直接の呼び出し関係を持たない。各保護 API の実行前に必ず通る**共通の関所**（C# の `[Authorize]` 属性/認証ミドルウェア相当。違いは物理的に別 Lambda=別プロセスで動く点）。
- **単独稼働は基本なし**: イベントソースは API Gateway のオーソライザ呼び出しのみ。EventBridge 定期実行・他サービスからの直接 invoke のような独立起動は設計に含めない。
- **毎回起動するわけでもない**: API Gateway は判定結果（トークン→Allow/Deny ポリシー）を **Authorizer result TTL** の間キャッシュする。TOKEN 型なので `Authorization` ヘッダ値がキャッシュキー。同一トークンで TTL 内の再アクセスは authorizer を起動せずキャッシュを使う。例外的な単独起動はコールドスタート初期化やテストの手動 invoke 程度。
- **未実装**: どのルートに attach するか/キャッシュ TTL は Terraform（Phase 1 `api_gateway` モジュール）で定義予定。現状コードは authorizer 自体（JWT 検証＋Allow/Deny ポリシー生成、`deny` は `Resource:"*"` の Deny・principalID=anonymous）のみ。

---

## 6. テスト（単体・結合）

### Q. 単体テストと結合テストは何が違う?
A.
- **単体テスト** … 外部依存なしで**ロジック単体**を検証。fake 実装やメモリ上のダミーで完結し、高速・安定。本プロジェクトでは `auth`(9) と `matchmaking`(16)。
- **結合テスト** … **本物の外部システム（LocalStack の DynamoDB）に接続**して、CRUD や原子的トランザクションが実環境で正しく動くか検証。`//go:build integration` タグ付きの `db`(4)。

### Q. matchmaking の grouping / matcher テストは何を行っている?
A.
- `select_group_test.go`（6 ケース）… `selectGroup` の**パーティ分割禁止 FIFO グルーピング**を検証。パーティが途中で割れない、ソロが混ざらない等。
- `matcher_test.go`（10 ケース）… `TryMatch` の**分岐フロー**を検証。チケット不在→エラー、非 WAITING→パススルー、期限切れ→タイムアウト処理、ソロ、通常成立(4 人)など。fakeStore + 固定時刻で判定。

---

## 7. 意図的な割り切り

スコープ外（仕様 4.8）: Backfill、スキルレーティング(MMR)、クエストカタログ(固定 5 種)、MatchmakingTickets の Global Tables、regionPref、EventBridge。
→ いずれも「**インフラ品質を見せる**」目的に対し本質でないため削ぎ落とした判断。

実装上の割り切り（自覚済みの改善余地）:
- `FindWaitingByPlayer` の冪等チェックは GSI の**結果整合**に依存（連打で稀に二重発行の隙間）。
- GameLift 接続情報・`playerSessionId` はスタブの固定値（Phase 3 で本物に差し替え）。
- Authorizer Context は flat keys（REST カスタムオーソライザの仕様上ネスト不可）。
- ログは `log.Printf` 直書き（Phase 6 で構造化）。

差し替え容易性: `SessionProvider` / `Store` インターフェースと `apigw.PlayerID` により、**簡略版を本番版へ無痛で挿せる構造**にしてある。

---

## 8. テストの実機確認

### Q. testify と testing の違いは?
A. `testing` は Go 標準のテストフレームワーク（`func TestXxx(t *testing.T)`）。`testify` はその上に**読みやすいアサーション**（`require`/`assert`）を足すサードパーティ。`require` は失敗で即中断、`assert` は継続。

### Q. テストの実機確認の方法は?（Windows での詰まり）
A. このマシンは **Windows Application Control (WDAC)** が `%TEMP%` 配下の大きい test バイナリ（crypto/HTTP を含む 12MB 級）を実行ブロックする。回避策:
```powershell
# %TEMP% を避け、プロジェクト配下にビルドしてから実行する
go test -c -o .\auth.test.exe .\internal\auth
.\auth.test.exe "-test.v"          # -test.v は必ずクォート（PowerShell が .v を分割解釈するため）

# 結合テストは integration タグ + LocalStack 起動が前提
docker compose -f docker/docker-compose.yml up -d localstack
bash ./docker/localstack/scripts/localstack-init.sh
go test -c -tags=integration -o .\db.test.exe .\internal\db
.\db.test.exe "-test.v"
```
- 小さい test（`matchmaking` ≒4MB）は `go test ./internal/matchmaking/...` で直接通る。
- 結合テストのテストデータは UUID で毎回ユニークにする（TTL 90 秒で残る前回データが GSI に引っかかるのを防ぐ）。

最終確認状態: 単体 25（auth 9 + matchmaking 16）+ 結合 4 すべて PASS。`go build ./...` / `go vet ./...` は常時通過。

---

## 9. Git / SSH 環境構築

### Q. Phase 0 で Git 管理を始めるのが抜けていた。Phase 0〜2 で漏れがないか精査したい。
A. 精査結果:
- 機械チェック … 未追跡ファイルゼロ、誤除外ゼロ（除外は tarball/exe/個人設定のみ）。
- ロードマップ照合で **Phase 0 の欠落** を発見 → 対応:
  - `.env.example`（0-3）を作成・コミット。
  - `develop` ブランチ（0-4）を作成・push。
  - 空の `.env/` ディレクトリは手動削除へ。
  - GameLift Local のコメントアウトは Phase 3-1 範囲で設計通り（問題なし）。

### Q. クローンは HTTPS でなく SSH で行いたい。汎用的にキーを作るのは問題?
A. 基本は汎用鍵 1 つで問題ない。**唯一の例外**は「同じ `github.com` に複数アカウントを SSH で使い分ける」場合。GitHub は 1 つの公開鍵を 1 アカウントにしか登録できないため、複数併用時は鍵を分けて `~/.ssh/config` のホストエイリアスで振り分ける必要がある。

### Q. キー作成コマンドの解説。
A.
```bash
ssh-keygen -t ed25519 -C "コメント(メール)" -f <出力パス>
```
- `-t ed25519` … 鍵アルゴリズム（現代の推奨）。
- `-C` … 識別ラベル（認証には無関係）。
- `-f` … 出力先。秘密鍵と `.pub`（公開鍵）の 2 つができる。
- パスフレーズは任意（`-N ""` で空固定にして非対話化も可）。
- **トラブル**: PowerShell の `ssh-keygen` は `~` を展開しないことがある → **フルパス**（`$env:USERPROFILE\.ssh\...`）で指定する。

### このマシンの最終 Git/SSH 構成
- リポジトリ: **`maedakai/RAIDYNC`**（SSH）。
- **複数アカウント環境**のため、衝突回避にホストエイリアスを採用:
  - `~/.ssh/config` の `Host github-maedakai` → 鍵 `~/.ssh/Github/maedakai/id_ed25519_raidync`。
  - origin URL = `git@github-maedakai:maedakai/RAIDYNC.git`。
- ブランチ戦略: `main`(デフォルト) / `develop`(開発統合) / `feature/*`。
- コミット作者はローカル設定で `Maeda` / `maeda.kai0314@gmail.com`。
- `.gitignore` で `server/go*.tar.gz`(64MB×2)・`*.exe`・`.env`・`.claude/settings.local.json` 等を除外。

---

## 10. Phase 3：ゲームサーバー / トランスポート

### Q. ゲームサーバーの通信は WebSocket(TCP) と UDP のどちらにすべき? 違いは?
A. **WebSocket は TCP の上に乗ったメッセージプロトコル**なので、選ぶと TCP の性質（到達保証・順序保証・再送）を受け継ぐ。
- **TCP/WebSocket** … 電話型。届くまで再送し順序も守る。ただし 1 パケット欠落で後続が待たされる（**Head-of-Line ブロッキング**）→ 20Hz 位置同期では「カクつき」要因。.NET の `TcpClient` 相当。
- **UDP** … ハガキ型。投げっぱなしで到達・順序保証なし。速いが、順序・欠落・輻輳制御を**自前実装**する必要（Unity Transport / Mirror の KCP が裏でやっていること）。.NET の `UdpClient` 相当。
- 位置同期は「50ms 前の古い座標は次パケットで上書きされる」ため再送の価値が薄く、アクションゲームでは UDP が定番。

**RAIDYNC の判断**: WebSocket も選択肢だったが、最終的に **UDP を採用**。理由は (1) 仕様 (spec-game.md §3.1) が GameLift UDP 7777 で一貫している、(2) アクションゲームの定番であり「なぜ UDP か」を技術面接で説明しやすい、(3) GameLift の UDP フリート前提と素直に噛み合う。トレードオフとして**信頼性レイヤを自前実装**する必要があるが、位置同期では「再送より最新値優先」が正しいため、フル ACK/再送ではなく **シーケンス番号による古い/重複パケットの破棄**だけを実装し、コストを最小化した。

### Q. UDP は順序保証がないのに、どう「古いパケット」を捨てている?
A. 共通エンベロープ (spec §3.4) に仕様外の拡張として `seq`（クライアントが 1 始まりで単調増加採番）を追加。サーバーは **プレイヤー単位で受理済み最大 seq を保持**し、`seq <= lastSeq` のパケットを黙って破棄する（`internal/game/session.go` の Ingest）。これで UDP の並べ替え・重複で古い座標に巻き戻る事故を防ぐ。SESSION_STATE 側もサーバー採番の seq を載せ、クライアントが同様に新しい方だけ採用できる。フル再送が要る将来要件（重要アクションの確実な到達）が出たら、この上に選択的 ACK を足せる設計。

### Q. 自作した信頼性レイヤを詳しく（何を実装し、何をあえて捨てたか）
A. 「信頼性」は実は 4 要素の集合（到達保証・順序保証・重複排除・輻輳制御）。TCP は全部やるが HoL ブロッキングを招く。今回はこのうち **位置同期に必要な「順序の逆転無効化＋重複排除」だけ**を seq ゲートで実装した。

- **前提**: 位置同期は「状態が冪等に上書きされ、古い情報は無価値」。50ms 前の座標は次パケットで完全に上書きされるので再送の価値が薄い。必要なのは「最新が届くこと」であって「全部が順番通り確実に届くこと」ではない（Mirror の unreliable channel と同じ考え）。
- **仕組み**: クライアントが seq を 1 始まりで単調増加採番（MOVE/ACTION 共通ストリーム）。サーバーは `if p.seen && msg.Seq <= p.lastSeq { return nil }` で破棄、通れば `lastSeq` 更新。
  - 並べ替え例: 送信 1,2,3 が到着 1,3,2 → ゲートなしは posC→posB で古い座標へ巻き戻る。ゲートありは 2<=3 で破棄し posA→posC で最新維持。
  - 重複例: seq3 が二重到着 → 2 回目は 3<=3 で破棄。
  - SESSION_STATE もサーバー採番 seq を載せ、クライアント側でも同ロジックで古いスナップショットを捨てられる（対称設計）。
- **あえて捨てたものと理由**: 再送（位置は上書きされる）/欠落補完（補間はクライアント責務）/輻輳制御（20Hz 固定レート）/ACK。
- **弱点と将来拡張**: (1) PLAYER_ACTION も再送なし＝重要イベントがベストエフォート → 冗長送信か選択的 ACK を足す。(2) 再接続で seq=1 に戻ると全部「古い」と誤判定 → 接続セッション ID で lastSeq リセット。(3) uint32 のラップアラウンドは 20Hz で約 6.8 年なので実用上無視。
- 要するに **TCP の信頼性を位置同期に必要な分まで削ぎ落としたコスト最小レイヤ**。フル信頼性が要れば KCP に置換し seq ゲートはそちらに吸収させる。

### 実装メモ（Phase 3 ステップ1〜4 完了）
- `pkg/protocol/game.go` … 共通メッセージ型（GameMessage エンベロープ + 各 payload、`seq` 拡張）。
- `internal/game/{player,session,sync,errors}.go` … セッション状態管理。Session が受信取り込み・seq ゲート・ダメージ適用・SESSION_STATE スナップショット生成・20 分タイムアウト/アイドル離脱を担当。Hub が複数セッションのルーティングと 50ms 周期ブロードキャストを担う。送信は `Sender` インターフェースで注入し net 依存をサーバー層に隔離（既存の matchmaking `Store` と同じ思想）。
- `internal/gameserver/server.go` + `cmd/gameserver/main.go` … 実 UDP ソケットと Hub の配線。main は薄いエントリポイント。
- テスト … session/sync のユニット 13 ケース + 実ループバック UDP の往復統合テスト（PLAYER_MOVE 送信 → SESSION_STATE 受信）。すべて green。
- **残**: ステップ5（GameLift Server SDK for Go 連携）／ステップ6（GameLift Local 接続確認）。GameLift SDK 依存と docker-compose の gamelift サービス有効化が必要。

### Q. 外部依存ゼロのほうがいい? メリット・デメリットと今回の候補は?
A. **「ゼロ」は目的ではなく手段**。今回はたまたま `net` + `encoding/json` で要件を満たせたのでゼロにしただけで、貫くこと自体に価値はない。判断軸は「何を自作して見せる価値があるか / 何を実績あるものに任せるべきか」。

- **外部依存を使うメリット**: 実績・最適化・エッジケース対応済み／開発が速い／機能が豊富。
- **デメリット**: サプライチェーン/脆弱性リスク／バージョン保守の手間／ブラックボックス化／肥大化／保守放棄リスク。
- **自作（ゼロ）のメリット**: 中身を全部説明できる（ポートフォリオ価値）／軽量・ビルド高速／サプライチェーン無関係。**デメリット**: 車輪の再発明／信頼性プロトコル全自作はバグの温床。
- .NET の類推: `Newtonsoft.Json` vs `System.Text.Json`、Mirror に任せる vs 自前 `UdpClient`、と同じトレードオフ。

今回（UDP ゲームサーバー）の外部依存候補:

| 領域 | 候補 | 備考 |
|------|------|------|
| 信頼性レイヤ | `xtaci/kcp-go`(KCP) / `quic-go`(QUIC) / ENet | KCP は **Unity Mirror の KCP Transport** と同系。順序保証+再送+輻輳制御を丸ごと任せられる |
| シリアライズ | Protobuf / FlatBuffers / MessagePack | JSON は 20Hz では冗長。帯域要件が出たらバイナリ化（Protobuf は .NET でも使える） |
| GameLift | `aws/amazon-gamelift-go-sdk` | ステップ5で**必須**。独自プロトコルで標準では代替不可 → 「依存ゼロ」はステップ4まで |
| ログ | `slog`(標準) / `zap` / `zerolog` | 構造化ログ（CloudWatch 連携）が要れば。まず標準 slog で足りる |

**RAIDYNC の線引き**: seq ゲート程度の軽い自作は「UDP 理解の証明」で価値があるので残す。順序保証+再送+輻輳制御のフル信頼性プロトコルを自作するのは過剰 → 必要になれば KCP（seq ゲートは KCP に吸収される）。シリアライズは可読性優先で当面 JSON、本番帯域（4,000 接続×20Hz）が問題化したら Protobuf（`Sender`/メッセージ型を分離済みなので差し替え可能）。GameLift SDK は不可避。「見せたいもの＝インフラ品質」に集中し、それ以外は実績あるものに任せるのが軸。

### Q. エンベロープ（envelope）とは何か?
A. メッセージを「**共通のメタ情報（封筒）**」と「**種別ごとに変わる中身（payload）**」に分ける設計パターン。封筒の比喩: 表面の宛名・差出人・種別は中を開けずに仕分けでき、中の手紙が実際の内容。

今回の `GameMessage` がエンベロープ（spec §3.4 の共通フォーマット）:
- 封筒側 = `type`（種別＝仕分けの鍵）/ `sessionId`（宛先）/ `playerId`（差出人）/ `timestamp` / `seq`（順序制御）
- 中身 = `payload`（type ごとに PlayerMovePayload / PlayerActionPayload 等に変わる）

**旨味**: UDP ソケットは 1 本で 4 種類のメッセージが混在する。封筒で統一すると共通処理が中身を知らずに済む — ルーティング(`Hub.Route`)は sessionId だけ、seq ゲートは seq だけ、ログは type だけを見ればよく、payload の中身から独立する。

**payload が `json.RawMessage` な理由 = 2 段階デコード**: (1) 受信バイト列を GameMessage にデコード（payload は封をしたまま生バイトで保持）→ (2) `type` で分岐 → (3) その時はじめて payload を正しい型へ Unmarshal。`interface{}` だと `map[string]interface{}` になり型安全を失うが、RawMessage なら後から 1 回で正しい型にできる。

.NET 類推: `JsonElement`/`JObject` を保持して種別確定後に `Deserialize<T>()` するのと同じ。type フィールドは **discriminated union の判別子(discriminator)**。C# と Go が同じ封筒形式を共有するので両言語で実装が揃う。

### Q. エンベロープは「クライアントとサーバーロジックをつなぐクラス」という認識でよい?
A. おおむね正しいが 2 点補正。
- **正**: クライアントとサーバーが共有するデータ構造。C# の DTO/POCO 相当。
- **補正1（ロジックは持たない）**: エンベロープは純粋なデータの入れ物。「つなぐ」のはフォーマットの合意であって処理ではない。役割分担は DTO（`GameMessage`/payload）＝データの形、Service（`Session.Ingest`/`Hub`）＝受信→状態更新のロジック。
- **補正2（クラス共有ではなくスキーマ共有）**: クライアント=C#、サーバー=Go と言語が違うので、1 つのクラスを DLL で共有しているのではない。各言語で同じ形のクラス/struct を別々に定義し、共通なのは **JSON フォーマット（契約 / スキーマ）= spec §3.4**。これを契約(contract)ベース/スキーマ駆動設計と呼ぶ。
- 伏線: Protobuf に移行すると 1 つの `.proto` から C#/Go のクラスを自動生成でき「単一定義から両言語が生まれる」状態になる。今は JSON なので両側に手で定義。
- 要約: エンベロープ＝両者が合意した「通信データの契約(DTO)」。つなぐのはロジックでなくフォーマット、共有するのはクラスでなくスキーマ。

### Q. Phase 3 各ファイルのシーケンス（処理の流れ）は?
A. 各ファイルのシーケンス上の役割:

| ファイル | 役割 |
|---|---|
| `cmd/gameserver/main.go` | 入口。設定読込・シグナル待受 |
| `internal/gameserver/server.go` | UDP I/O。受信ループ・送信(udpSender)・tick 駆動 |
| `internal/game/sync.go` (Hub) | ルーティング(Route)・周期処理(Tick)・ブロードキャスト |
| `internal/game/session.go` (Session) | 1 セッションの状態遷移。Ingest/Snapshot/lifecycle |
| `internal/game/player.go` (player) | 1 プレイヤーの状態。snapshot/applyDamage |
| `pkg/protocol/game.go` | 全層が運ぶデータ型（エンベロープ/payload） |
| `internal/game/errors.go` | 異常系の合図（ErrNotInRoster 等） |

**起動フロー**: main.loadConfig → gameserver.New(ListenUDP→NewHub→NewSession→Register) → Serve(go broadcastLoop + receiveLoop)。

**受信フロー**（下り）:
```mermaid
sequenceDiagram
    participant U as Unity(C#)
    participant SV as server.go receiveLoop
    participant H as sync.go Hub
    participant S as session.go Session
    participant P as player.go
    U->>SV: UDP datagram (PLAYER_MOVE/ACTION)
    SV->>SV: ReadFromUDP / json.Unmarshal → GameMessage
    SV->>H: Route(msg, addr, now)
    H->>H: session(msg.SessionID) 取得 (無→ErrSessionNotFound)
    H->>S: Ingest(msg, addr, now)
    S->>S: roster検証(外→ErrNotInRoster) / seqゲート / addr,lastSeen更新
    S->>P: MOVE→位置更新 / ACTION→applyDamage
    S-->>SV: err (あればlogでdrop)
```

**ブロードキャストフロー**（上り, 50ms 周期）:
```mermaid
sequenceDiagram
    participant T as ticker(50ms)
    participant SV as server.go broadcastLoop
    participant H as sync.go Hub
    participant S as session.go Session
    participant P as player.go
    participant TX as udpSender
    participant U as Unity(C#)
    T->>SV: tick
    SV->>H: Tick(now)
    loop 各セッション
        H->>S: PruneIdle / EndReason
        alt 20分到達
            S-->>H: SESSION_END + 登録解除
        else 継続
            H->>S: Snapshot(now)
            S->>P: 各 player.snapshot() 集約
            S-->>H: GameMessage(SESSION_STATE, outSeq++)
        end
        H->>H: json.Marshal(1回)
        H->>TX: SendTo × recipients
        TX->>U: WriteToUDP
    end
```

**対称性**: 下り Unity→server→Hub.Route→Session.Ingest→player(書く)、上り player(読む)→Session.Snapshot→Hub.broadcast→udpSender→Unity。Hub を境に net 側とロジック側が分離し、`Sender` IF だけが境界をまたぐ。

### Q. 20 分でセッションがタイムアウトするのは GameLift の特性?
A. **GameLift の特性ではない**。ゲーム設計上の制約（モンハン型クエスト 1 回の制限時間）をアプリ側で自前実装したもの。
- 出所: `CLAUDE.md`「最大 20 分」/ `spec-game.md §1.2`。実装は `protocol.MaxSessionMinutes=20` → `session.go` の `endsAt = createdAt + maxSessionMs`、`EndReason` が `now >= endsAt` を判定。
- GameLift 自体は「プレイ時間の上限」を持たない。セッションはゲームサーバープロセスが終了させるまで続く。GameLift が持つのは別種のタイムアウト＝(1) ゲームセッション起動タイムアウト(ACTIVATING→ACTIVE, 設定可能) (2) プレイヤーセッション予約タイムアウト(RESERVED→TIMEDOUT, 接続猶予 数十秒)。いずれも起動・接続まわりでありプレイ時間ではない。
- **セッション終了はアプリ責務**: 終了条件(20分到達/クリア/全員離脱)を満たしたらサーバーが (1) クライアントへ SESSION_END 送信（今の `EndMessage` が担当, 完成済み） (2) GameLift SDK の `ProcessEnding()` 呼び出し→インスタンスを次セッションに再利用（**ステップ5で接続予定**）。`EndReason`/`EndMessage` に切り出したのは ProcessEnding を差し込めるようにするため。
- 整理: 「20分」=アプリ制約。「いつ終わらせるか」=ゲームサーバーの責務。GameLift は終了後のインスタンス再利用を担う。

### 実装メモ（Phase 3 ステップ5：GameLift Server SDK 連携 完了）
- 依存: `github.com/amazon-gamelift/amazon-gamelift-servers-go-server-sdk`（SDK 5.x, 2025-05 公開。pseudo-version）。推移的に gorilla/websocket, go-retry 等を SDK が利用。
- `internal/gameserver/gamelift.go` … `GameLiftBridge` が SDK ライフサイクルを Hub に橋渡し。`Activate`(InitSDKFromEnvironment→ProcessReady) / `onStartGameSession`(roster抽出→NewSession→hub.Register→ActivateGameSession) / `onProcessTerminate`(stop=Spot中断対応) / `onHealthCheck`(true) / `End`(ProcessEnding)。
- **roster 受け渡し**: RAIDYNC は FlexMatch 非使用（自前 Lambda マッチメイキング）なので `model.GameSession.GameProperties`（map[string]string, `questId`/`playerIds`）で受け渡す。MatchmakerData は使わない。`parseSessionConfig` が純関数で抽出しユニットテスト。
- **ローカルファースト維持**: `GAMELIFT_ENABLED=false`（既定）では InitSDK を呼ばず AWS なしで動く。SDK はビルドに含むが実行時呼ばない。`server.go` の `Config.GameLiftEnabled` で固定セッション登録を分岐、`main.go` の `GAMELIFT_ENABLED` で Bridge 起動を分岐。
- テスト方針: SDK 本体はグローバル状態でモックしづらいため、ロジック(parseSessionConfig)を純関数に切り出して検証。SDK 呼び出しは薄いアダプタに隔離。

### Q. GameLift Local と GameLift Anywhere の違いは? なぜ Anywhere を使う?
A. **GameLift Local は SDK 5.x で非推奨**。AWS は **GameLift Anywhere** に置き換えた（旧 GameLift Local は SDK 3.x/4.x 専用の Java エミュレータ）。
- GameLift Local: ローカル Java プロセスで GameLift API をエミュレート。オフライン・無料だが SDK 5.x 非対応で事実上終了。
- GameLift Anywhere: 自前マシン（ローカル PC やオンプレ）を GameLift の「Anywhere フリート」として登録し、実際の GameLift コントロールプレーンと接続して検証する仕組み。**AWS アカウントでのフリート登録が必要＝コストが発生**。
- 影響: CLAUDE.md / spec / docker-compose の「GameLift Local」前提は古い。ステップ6「接続確認」は Anywhere 前提に読み替え。ローカルファースト/月 $200 制約との兼ね合いで、ステップ6は最小限実施 or 保留の判断が必要。

### Q. GameLift Local が廃止された経緯と、Anywhere との実態の違いをもう少し詳しく
A. 「廃止」の中身と、代替の Anywhere が *ローカルだが無料オフラインではない* 点が要点。

- **廃止の正体**: 旧 GameLift Local は `GameLiftLocal.jar`（Java エミュレータ）。手元で起動すると GameLift API を模した HTTP/WebSocket が立ち、AWS アカウントもネットワークも使わず `ProcessReady` / `CreateGameSession` を検証できた。これが **Server SDK 5.x と非互換**。Local は **SDK 3.x / 4.x 専用**として据え置き、5.x ユーザーは Anywhere に誘導される（AWS 公式 doc も Local の項を「SDK 4.x 以前向け」と明記）。
- **なぜ非互換か**: SDK 5.x で接続が **WebSocket ベースに刷新**＋ `authToken` 必須化。旧 Local（古い REST 寄り実装）が追従しておらず、AWS はローカル専用実装の維持をやめ「本番と同じコードパスで動く Anywhere」に一本化した。
- **Anywhere の実態**: 「自分の PC をそのまま GameLift フリート1台として AWS に登録」する方式。ゲームサーバーのバイナリは手元で動くが、`CreateGameSession` 等の制御は **実 AWS の GameLift サービス経由**。

| 観点 | GameLift Local（旧） | GameLift Anywhere（新） |
|------|---------------------|------------------------|
| AWS アカウント | 不要 | **必要** |
| ネットワーク | 完全オフライン可 | AWS 接続必須 |
| 必要リソース | JAR 1個 | カスタムロケーション + Anywhere フリート + compute 登録 |
| 認証 | なし | `get-compute-auth-token` で短命トークンを定期取得 |
| `InitSdk()` 引数 | 不要 | `webSocketUrl` / `hostId` / `fleetId` / `authToken` / `processId` 必須 |
| サーバー動作場所 | ローカル | ローカル PC のまま（compute 登録） |

- **コスト**: Anywhere フリート *自体* のインスタンス課金は無し（自前ハード利用）。ただし AWS アカウント・IAM・API コールは発生し、**完全オフライン/コストゼロではない**。CLAUDE.md の「AWS デプロイ前に Local で確認」前提と docker-compose の gamelift コンテナは実体を失う。
- **実装との接続**: `gamelift.go` の `InitSDKFromEnvironment()` は、Anywhere では上記5パラメータ（`webSocketUrl` 等）を環境変数から読む形になる。managed EC2 フリートへ移行する際は **これらを外すだけ**（AWS が自動注入）でコード差分が最小になる設計。`GAMELIFT_ENABLED=false`=ローカル固定セッション / `true`=Anywhere・managed の切替は妥当。

> 出典: [Server SDK 5.x for Amazon GameLift Servers](https://docs.aws.amazon.com/gameliftservers/latest/developerguide/reference-serversdk.html) / [Set up local testing with Amazon GameLift Servers Anywhere](https://docs.aws.amazon.com/gameliftservers/latest/developerguide/integration-testing.html) / [GameLift Local not available on SDK 5.0.0?（re:Post）](https://repost.aws/questions/QUGtPVWmd9SSmfgdscxMJ9rg/gamelift-local-not-available-on-sdk-5-0-0)

### Q. GameLift Local は UDP ではできない（UDP 非対応）ということ?
A. 違う。**GameLift Local の廃止は UDP と無関係**。コントロールプレーンとデータプレーンを分けて考えるのが肝。

- **コントロールプレーン（GameLift の担当）**: ゲームサーバー ⇄ GameLift 間のライフサイクル（`ProcessReady` / `CreateGameSession` / health check / `ProcessEnding`）。通信は **HTTP/WebSocket（TCP）**で、UDP は使わない。
- **データプレーン（GameLift 非関与）**: Unity クライアント ⇄ ゲームサーバー間の実ゲーム通信（PLAYER_MOVE / SESSION_STATE）。**ここが自作 UDP**。GameLift はクライアントに IP:Port を教えるだけで、以後の UDP パケットは経由しない。
- 従って **Local は UDP を中継/エミュレートする機能をそもそも持たない**。「UDP 非対応で廃止」ではなく、コントロールプレーンの通信が SDK 5.x で WebSocket 化したことに Local が追従していないだけ。
- **帰結**: UDP ゲーム通信のローカル検証は GameLift 無しで完結する（= ループバック UDP 往復テストで実施済み）。GameLift（Local/Anywhere）が要るのは**ライフサイクル制御だけ**。

| 検証対象 | GameLift 要否 | 現状 |
|----------|--------------|------|
| UDP ゲーム通信（移動同期・ブロードキャスト） | 不要 | ✅ ローカル検証済み |
| セッションのライフサイクル（起動/終了/health） | 必要 | ❌ Local 廃止 → Anywhere(AWS) が必要 |

### Q. Anywhere を使えば「本番想定と同じ形」の検証ができる?
A. **アプリ/統合レイヤは本番相当でできる。インフラ/運用レイヤは managed フリートでないと検証できない**、という住み分け。Anywhere は旧 Local より本番に忠実（実コントロールプレーンに接続・SDK コードパス同一）だが managed と完全イコールではない。

**本番と同じ形で検証できる（アプリ/統合）**
- SDK コードパスが managed と同一: `InitSdk → ProcessReady → OnStartGameSession → ActivateGameSession → ProcessEnding`、WebSocket+authToken も同じ。
- **エミュレータでなく実 GameLift コントロールプレーンに接続**（Local は模造品だった）。`CreateGameSession`/`DescribeGameSessions` の挙動が本物。
- queue / matchmaker を managed にそのまま流用可能。
- 今回の `gamelift.go` の `OnStartGameSession`(roster 取り出し→Hub登録) / `OnProcessTerminate`(畳み) の正しさは本番同形で確認可。

**本番と違う（インフラ/運用）— managed フリート必須**

| 項目 | Anywhere | managed（本番） |
|------|----------|----------------|
| `InitSdk()` 引数 | 手動指定 | AWS 自動注入 |
| ビルド配置 | 手動デプロイ | S3 経由で自動 |
| authToken | 自前で定期取得 | AWS 自動管理 |
| Spot 中断 | 自然発生しない | 実発生→`OnProcessTerminate` |
| オートスケール / 容量 | 無し | 自動増減 |
| 実行環境 | 自分の PC | 本番 EC2 種別 |
| ネットワーク | ローカル | Route53 レイテンシルーティング・マルチリージョン |

- 帰結: Anywhere = 統合テストは本番相当。**Spot 中断挙動・スケール・配置・ネットワークの検証は managed フリート（Phase 4 以降）が必須**。RAIDYNC は「インフラ品質」が主題なので、最終的に managed/Spot まで触れて初めてその領域が本番検証できる。

### Q. Anywhere を使うコストは具体的にいくら?
A. **Anywhere 自体はほぼ無料**（無料枠内 $0、超えても dev 検証は月数十円）。**インスタンス時間課金が無い**（自前ハードを使うため）のが肝。高額化するのは後の managed フリート（EC2 課金）であって Anywhere ではない。

**課金体系**

| 課金対象 | 単価 |
|----------|------|
| ゲームセッション配置 | $0.0012 / セッション |
| サーバープロセス接続時間 | $0.000011 / 分 |
| Anywhere フリートの compute（=自分の PC） | $0（課金なし） |
| ゲーム通信のデータ転送（client⇄server 直接・AWS 非経由） | $0 |

- **無料枠（12か月）**: 月 3,000 セッション配置 + 500,000 接続分（全リージョン合算）。

**RAIDYNC dev 検証の試算**
- 20分セッション1回 ≈ $0.0014（配置 $0.0012 + 接続 20分×$0.000011）。
- 開発中サーバー常時起動（8h×20日=9,600分）≈ $0.11、セッション200回でも +$0.24 → 月 ~$0.35、かつ無料枠内で実質 $0。
- 24時間×1か月放置（最悪）= 43,200分×$0.000011 = $0.475 → これでも無料枠 500,000分内。
- → **$200 budget リスクはほぼゼロ。止め忘れても月 $0.5 未満**。

**周辺費用の注意**: CloudWatch にログ/メトリクスを送れば微少課金（送らなければ $0）。`get-compute-auth-token`・IAM は無料。

**本当にコストが出るのは managed フリート（対比）**

| | Anywhere | managed EC2 フリート |
|--|----------|---------------------|
| インスタンス課金 | 無し（自前 PC） | EC2 時間課金（起動中ずっと） |
| 消し忘れリスク | ほぼ無害（月 $0.5） | 高額化しうる |

- CLAUDE.md「フリートは使わない時必ず停止」は **managed フリート向け**の制約で、Anywhere には実質当てはまらない（止め忘れても安い）。
- 帰結: ステップ6で Anywhere を試すのはコスト的に十分許容範囲。要るのは AWS アカウント設定とフリート登録の手間のみ。

> 出典: [Amazon GameLift Anywhere Pricing](https://aws.amazon.com/gamelift/pricing/anywhere-pricing/)

### Q. GameLift Anywhere は「ローカル環境の構築に必要」という認識でいい?
A. 半分正しく半分は補正が必要。「ローカル環境の構成部品/前提」という意味なら**誤り**。「ローカルマシンで GameLift 統合を検証する手段」という意味なら**正しい**。「ローカル」が2義で混ざりやすい。

- **オフラインのローカル環境（Docker/LocalStack スタック）の構成部品か → No**: RAIDYNC のローカル環境 = LocalStack + mock-auth +（UDP サーバーの Go 単体テスト）。**Anywhere 無しで完全に動く**。UDP 通信は GameLift 非依存でローカル検証済み。Anywhere は Docker スタックに含まれない実 AWS リソースで、LocalStack のようなオフライン代替品ではない。
- **ローカルマシン上でテストする仕組みか → Yes**: Anywhere の目的は「自 PC でサーバーを動かしつつ GameLift 統合を反復テスト」。旧 GameLift Local（ローカルテスト用）の後継。ただし **実行=ローカル / 制御プレーン=実 AWS** のハイブリッドで、オフラインでもコストゼロの閉じた環境でもない。

| 問い | 答え |
|------|------|
| ローカル環境（オフライン）の構築に必要か | 不要（無くても動く・構成部品でもない） |
| ローカルマシンで GameLift 統合を検証する手段か | その通り（GameLift Local の後継） |
| オフライン・コストゼロか | 違う（実 AWS 接続・無料枠内だが AWS アカウント必須） |

- 帰結: 今回 Anywhere を作る目的は「ローカル環境を構築するため」ではなく「**ローカルで完成させたゲームサーバーが実 GameLift と正しく会話できるか検証するため**」。位置づけは「ローカル開発の延長にある統合テスト」であって、ローカル環境の土台ではない。

### Q. Anywhere が無くても、現状のローカル環境でゲームに必要なサーバー処理は揃う?
A. 「リアルタイムのゲーム処理（データプレーン）」を指すなら**揃っている**。ただし「ゲームに必要なサーバー処理」を広く取ると、GameLift が担うホスティング制御と、マッチメイキング接続がまだ無い。

**揃っている（実装済み・GameLift 非依存・ローカル検証済み）= データプレーン**
- UDP 受信ループ / seq ゲート（古い・重複破棄）/ 位置・状態同期（PLAYER_MOVE）/ アクション・ダメージ・死亡（PLAYER_ACTION）/ 20Hz ブロードキャスト（SESSION_STATE）/ タイムアウト・アイドル切断 / セッション終了。
- 「受け取って・更新して・配る」ゲームループは完結。

**揃っていない（GameLift = コントロールプレーンの担当）**
- セッション起動指示（OnStartGameSession）/ health check / スケールダウン時の graceful 終了（OnProcessTerminate）/ 配置。
- ローカルモードではこれを **env roster の固定セッションで擬似代替**しているだけで本物のオーケストレーションは無い。

**もう一つの未接続点**
- マッチメイキング（Lambda）とゲームサーバーは現状繋がっていない。Lambda は `gamelift_stub` がダミー接続先を返すだけ、サーバーは env roster で固定起動。「マッチ成立→そのメンバーで実セッション起動」の継ぎ目は GameProperties 経由＝GameLift モードでしか流れない。これを実物で繋ぐのがプランB（Anywhere）と Phase 4。

- まとめ: ゲームの**リアルタイム処理ロジックの作り込みはローカルで完結**。足りないのは「誰が・いつ・どのメンバーでセッションを立て、どう畳むか」のホスティング制御とマッチメイキング接続で、これが GameLift（Anywhere/managed）の領域。

---

## 11. Phase 4：ローカル E2E ハーネス

### Q. Phase 4 で結局なにをする?
A. これまでローカルで個別に作った部品（認証・マッチメイキング・ゲームサーバー）を **1 本のフローに繋いで E2E で完走を確認する**フェーズ。通すフローは:
```
ログイン → マッチリクエスト → マッチ成立 → ゲーム開始 → 結果保存 → 履歴取得
（mock-auth）  （match API）              （gameserver）           （MatchHistory）
```
ロードマップ項目 4-1（マッチ API を動かす）/ 4-2（client と結合）/ 4-3（結果保存・履歴取得）/ 4-4（手動 E2E 完走）。**部品は揃っているので「繋ぎ方」と「結果保存」だけ新規に決めて作る**のが本質。

### Q. ロードマップは「AWS dev で E2E」だが、ローカルで閉じてよい?
A. 今回は **ローカルファースト方針を優先し、AWS を使わずローカル E2E に閉じる**判断。`mock-auth` + LocalStack（DynamoDB）+ ローカル gameserver で全フローを検証する。理由は (1) これまで Phase 0〜3 を一貫してローカルファーストで進めてきた整合性、(2) AWS コスト・手間の最小化、(3) ロジックはローカルで完成済みで、AWS 反映（Terraform 化）は別トラックとして切り出せるため。実 AWS への反映と GameLift Anywhere 接続検証は後続（Terraform 整備とまとめて）に回す。

### Q. 着手前に見つかった「2 つのギャップ」とは?
A.
- **ギャップ① Lambda ロジックにローカル HTTP で到達する手段が無い**: `localstack-init.sh` は DynamoDB テーブルを作るだけで、Lambda も API Gateway も LocalStack に未デプロイ。さらにハンドラは全て `package main`（デプロイ前提）のため、**そのままでは client から HTTP で叩けない**。→ 解決策はトランスポート方式の選定（下記）。
- **ギャップ② ゲーム結果を DynamoDB に保存する本番パスが無い**: `db.PutMatchResult` は実装済みだが**それを呼ぶコードがどこにも無い**。gameserver は DynamoDB 非依存で、`SESSION_END` 時に履歴を書く経路が存在しない。→ Phase 4 で「結果レポート経路」を新規実装する必要がある（誰が SESSION_END 時に MatchHistory を書くか＝設計判断が残る）。

### Q. ローカル E2E で Lambda ロジックをどう駆動する? 3 案の違いと採用案は?
A. **採用＝案 A（ローカル API バイナリ + CLI client）**。3 案はいずれも「各 Lambda の `handle` を `package main` から `internal/handlers/*` へ抽出してトランスポート非依存にする」のが共通前提。違いは抽出したハンドラを**何経由で叩くか**。

| 観点 | A: localapi+CLI | B: go test E2E | C: LocalStack 実デプロイ |
|------|----------------|----------------|-------------------------|
| 構成 | `cmd/localapi`(net/http) + `cmd/client`(CLI) | `test/e2e`(build tag) で handler を直接呼ぶ | init を拡張し zip+API GW を LocalStack 登録 |
| 本番構成への忠実さ | 高 | 中 | 最高 |
| 動くデモが残る | ◎ | ✕ | ◎ |
| CI 回帰テスト | △ | ◎ | △ |
| 実装量 | 大 | 小 | 特大 |
| 過剰設計リスク | 中 | 低 | 高 |

- **A を選んだ理由**: 転職用ポートフォリオとして「`docker compose up` 後に**実際に動く全フロー CLI デモ**が残る」こと、**本番の API Gateway + Lambda 構成に最も忠実**（ハンドラが実際に `APIGatewayProxyRequest/Response` をやり取りする）であること。`localapi` は API Gateway proxy 統合を再現する薄い層、`client` は将来の Unity クライアント参照実装にもなる。
- **B/C の位置づけ**: B は最軽量・CI 友好だが「動く画面デモ」が残らない。C は最も忠実だが配線が重く、本来 Terraform でやるべき API Gateway 配線を bash で二重化する懸念（過去 feedback「ローカル専用ツールを過剰設計しない」に最も抵触）。

### 実装メモ（Phase 4 ステップ1：ハンドラの importable 抽出 完了）
- `internal/handlers/` を新設し、4 つの proxy ハンドラ本体を移設（`match_request` / `match_status` / `match_cancel` / `match_history`）。各々 `New*(deps)` コンストラクタ + `Handle(ctx, req)`（API Gateway proxy signature）を公開。
- 本番 `cmd/lambda/{match_request,match_status,match_cancel,match_history}/main.go` は、依存を組んで `lambda.Start(h.Handle)` するだけの薄い殻に変更（**挙動は不変**）。これで本番 Lambda と後続の `cmd/localapi` が**同一のハンドラ実装を共有**する。
- `authorizer` は別統合型（TOKEN 型カスタムオーソライザ）のため未変更。`localapi` 側では `internal/auth` の Verifier を**認証ミドルウェアとして直接利用**する方針（authorizer Lambda 本体は再利用しない）。
- 検証: `go build ./...` / `go vet ./...` 通過、ロジック系ユニットテスト（matchmaking / game）回帰なし。
- **残**: ステップ2（`cmd/localapi`）／ギャップ②の結果保存パス設計／ステップ3（`cmd/client`）／E2E 完走確認。

### Q. `cmd/client` が使う `id_token` は JWT とは別もの?
別ものではない。**`id_token` は JWT そのもの**。「JWT」はトークンの**フォーマット**名（`ヘッダー.ペイロード.署名` を Base64URL で繋いだ文字列、署名で改ざん検知できる）、「ID トークン」はその形式で作った**用途**名（OIDC 用語で「誰がログインしたか」を表す）。レイヤーが違うだけ。
- mock-auth の `issueTokens` は 2 種類の JWT を発行する：
  - **id_token**（`token_use:"id"`）… sub / email / aud を含む。「誰か」を表す。
  - **access_token**（`token_use:"access"`）… sub / scope / client_id を含む。「何ができるか」を表す。
  - どちらも RS256 署名の JWT で、違いは**中のクレームだけ**。
- OIDC のセオリーでは API 認可は access_token だが、本プロジェクトの `internal/auth.Verifier` は **`sub`（playerId）が取れればよい**作りのため、sub を持つ id_token でも通る。`cmd/client` は当面 id_token を「sub を運ぶ JWT」として使う。将来 access_token に寄せるなら Verifier 側で `token_use=="access"` を要求する整理になる。

### Q. `POST /session/{sessionId}/end` は「GameLift でゲーム終了しプレイヤーを GameLift から切る処理」?
半分そう、半分違う。このエンドポイントの実体（`session_end.go`）は **アプリ/ビジネス側の終了処理だけ**：①roster 各人へ `MatchHistory` を1件ずつ書く（`PutMatchResult`）／②**自前の** `Sessions` テーブルを `ENDED` に（`EndSession`）。**GameLift の制御プレーンには触れない。**
- 「プレイヤーを GameLift から切る／枠を解放する」のは GameLift Server SDK 5.x の別呼び出し：`RemovePlayerSession(playerSessionId)`（枠解放）→ `ProcessEnding()`（プロセス終了通知）。今回は未実装（GameLift Anywhere 実接続時の範囲）。
- 本番のプロセス終了時は **①結果報告（=今回の /session/end）と ②SDK の RemovePlayerSession→ProcessEnding の両方**を呼ぶ。今回は①だけ。
- ローカルハーネス特有の割り切り：/session/end は DynamoDB しか触らないので、同居 Hub 上のインメモリ `game.Session` はこの呼び出しでは解除されない（Hub 自身の 20分タイムアウト/abandoned で終了）。E2E 確認は DynamoDB の結果保存が見えれば十分なので現状はこれでよい。

### Q. ゲーム終了処理は GameLift 自体が行い、GameLift が session/end と RemovePlayerSession→ProcessEnding を呼ぶ?
逆。**呼ぶのは「GameLift 上で動くあなたのゲームサーバのコード」**。GameLift（プラットフォーム）が勝手にこれらを呼ぶのではない。
- 制御の向き（通常終了）：ゲーム終了を判断するのは**あなたのプロセス** → ①あなたの backend へ `POST /session/end`（結果を DynamoDB へ）／②同インスタンス上の GameLift Agent へ SDK 経由で `RemovePlayerSession`→`ProcessEnding`。GameLift プラットフォームは②を**受けて**プロセス/枠を回収する側で、独自 API の session/end は知らない。
- 例外（逆向き）：Spot 回収・フリート縮退時は GameLift が `OnProcessTerminate` コールバックであなたのプロセスに「もうすぐ落とす」と通知。受けたあなたのコードが急いで片付け（結果保存・ProcessEnding）を**自分で**行う。実行主体は常にあなたのコード。

### Q. サーバーロジックは GameLift 自体ではなく、GameLift 内のインスタンスにデプロイする?
その通り。**「GameLift という箱」ではなく、GameLift が管理する EC2 インスタンス群（フリート）の各インスタンスに配置されて動く。**
- デプロイ階層：`Go ビルド` → **Build**（GameLift に登録した成果物、まだ動かない）→ **Fleet**（この Build を EC2 タイプ/Spot/台数で動かす定義）→ GameLift が **EC2 インスタンス**をプロビジョンし Build を自動配置 → 各インスタンスの **GameLift Agent が実行ファイルをプロセスとして起動**（1 プロセス ≒ 1 ゲームセッション/最大4人）。
- 「GameLift 自体」はマネージドな制御層（オーケストレーター）。あなたは Build を渡すだけで、EC2 の調達/パッチ/スケール/Spot 回収を GameLift が肩代わりする。だから `ProcessEnding()` は「インスタンス上のあなたのプロセス → 同インスタンスの GameLift Agent」へのローカル呼び出しになる。
- Fleet には Build に加えランタイム設定（`runtime_configuration`）を与える：launch path（起動する実行ファイル）/同時起動プロセス数/ServerSdkVersion。Terraform は `aws_gamelift_fleet`。
- フリート種別：**Managed EC2**（本命）／コンテナ（イメージを配る）／**Anywhere**（EC2 の代わりに自前マシン=自 PC を compute として登録）。Phase 4 の Anywhere は「EC2 の役を自 PC が担う」位置づけ。
- 本プロジェクト対応：Build=`cmd/gameserver`（GameLift SDK 連携済み Go バイナリ）／Fleet=Terraform `modules/gamelift`（Spot 主・On-Demand 従）／ローカル代替=今回の同居ハーネス（`localapi` 内で Hub を動かし EC2/Fleet を立てずプロセスだけ再現）。

### 実装メモ（Phase 4 ステップ2〜3：ローカル E2E 完走 完了・2026-06-25）
- **ステップ2 `cmd/localapi`**: API Gateway + 各 Lambda の代役 HTTP サーバ。`internal/handlers` を本番 Lambda と共有し、`adapt`（http↔API Gateway proxy 変換）でマウント。`withAuth` ミドルウェアが Bearer JWT を `internal/auth.Verifier` で検証し sub を Authorizer Context へ注入（authorizer Lambda の代役）。
- **案A 同居**: `cmd/localapi/gameserver.go` の `hubSessionProvider`（`matchmaking.SessionProvider`）が UDP ゲームサーバ（`GameLiftEnabled:true`）の Hub に、マッチ成立時の roster でセッションを動的登録（GameLift `OnStartGameSession` の代役）。
- **ギャップ②解消**: `POST /session/{sessionId}/end`（`handlers.SessionEnd`）が roster 各人へ `MatchHistory` を書き（`PutMatchResult`）、自前 `Sessions` を ENDED に。server-to-server 経路のため `withAuth` の外にマウント。
- **ステップ3 `cmd/client`**: E2E ドライバ。signup（409 許容）/login→match/request（`soloAllowed:true` で即成立）→status ポーリング→UDP play（PLAYER_MOVE×N + PLAYER_ACTION、SESSION_STATE 受信）→session/end→history。roster 照合用 playerId は id_token の sub から取り出す（`subjectFromJWT`）。
- **E2E 完走確認**: `docs/phase4-e2e-runbook.md` の手順で実機完走（`client: E2E completed`）。AppControl ブロックは発生せず。
- **残**: GameLift Anywhere 実接続（自 PC を compute 登録、SDK の AcceptPlayerSession/RemovePlayerSession/ProcessEnding 経路）。これは Phase 4 後半 or 別途。

### Q. ローカル E2E が通っているのに、なぜ GameLift Anywhere 実接続までやる?
ローカルハーネスは GameLift を `hubSessionProvider` で**スタブ化**しており、**GameLift SDK との結線（契約）が一度も動いていない**ため。
- ローカル E2E が保証するのは**自分のアプリのロジック**（マッチング・UDP・結果保存）。GameLift の `InitSDK→ProcessReady→OnStartGameSession(roster)→AcceptPlayerSession→ProcessEnding`・ヘルスチェックは未実行。
- GameLift Anywhere は**自 PC を Anywhere フリートの compute として登録**し、サーバーバイナリはローカルで動かしつつ GameLift 制御プレーンとは本物で通信する方式。これで未検証の SDK 経路を本物で通せる（署名違い・呼び忘れはここでしか出ない）。
- ローカルで実接続する意味＝**EC2 を立てず無料枠で安く・速く**回せる。SDK 経路が Anywhere で通れば、Managed EC2 への移行はほぼ設定だけ（Build アップロード+Fleet 定義）になり、本番デプロイのリスクを先に潰せる。
- 役割分担：**ローカル E2E=ロジック検証／Anywhere 実接続=GameLift SDK 契約検証**。

### Q. AWS dev E2E では何をする?
**Terraform（`terraform/environments/dev`）で実 AWS 上に構築した dev 環境**で、全マネージドサービスを本物にして繋ぎ、本番一歩手前の統合検証を行う。
- 本物に置き換わるもの：認証=実 Cognito（mock-auth→）/API=実 API Gateway+Lambda（localapi→、authorizer・IAM・コールドスタート含む）/DB=実 DynamoDB（LocalStack→、IAM・GSI・TTL・スロットリング）/ゲームサーバ=実 GameLift Managed EC2 フリート（同居 Hub→、サーバ自身も GameLift 起動の EC2 上で動く）/ネットワーク=Route53・SG・実 UDP 到達性/観測=CloudWatch・X-Ray。
- ここでしか出ない問題：**IAM の実効性**（最小権限で本当に動くか）、マネージドサービスの実挙動（結果整合性・スロットル・実 JWT/JWKS・API GW 制限・Lambda 実行環境差）、サーバが GameLift 管理 EC2 上で動く本番同等経路、全サービス間の E2E 結線。
- 段階：ローカルE2E（ロジック）→ Anywhere（SDK 契約・サーバは自PC・無料枠）→ **AWS dev（全サービス実物・統合）** → 負荷テスト(Phase 6) → マルチリージョン/prod。

### Q. 従量課金はどこから発生する?
**実課金が始まるのは AWS dev を `terraform apply` した瞬間**。local=ゼロ、Anywhere≈ゼロ（無料枠・compute は自 PC で EC2 課金なし）、dev から実課金。ただし「従量だけ」ではない点に注意：
- **A. 従量課金（使った分）**: API Gateway（リクエスト数）/Lambda（実行×GB秒）/DynamoDB（PAY_PER_REQUEST=読み書き回数）/Cognito（MAU）/CloudWatch・X-Ray/データ転送。叩かなければほぼ増えない（無料枠あり）。
- **B. 立てているだけで掛かる（固定/時間課金）← 注意**: GameLift Managed EC2 フリート（**プレイヤー0でも ACTIVE な間ずっとインスタンス時間課金**。最大のコスト源）／NAT Gateway（時間課金、概算 ~$0.045/時≈月$30 強、region 依存）／Route53 ホストゾーン（月額固定 概算 $0.50/ゾーン）。※金額は概算・region/時期で変動。
- 直結する制約：CLAUDE.md「**GameLift フリートは使わないとき必ず停止**」「Budgets 月 $200」。運用は **dev は必要時だけ apply→終わったら destroy（特にフリート/NAT）**、ローカル・Anywhere で潰せる検証は先に潰し **dev 滞在を最短化**。Anywhere を挟む価値は「課金が本格化する dev の前に無料で SDK 結線を潰す」点にもある。

### Q. GameLift Anywhere 実接続と AWS dev E2E は両方必要?
**「両方必須」ではない。** カバレッジで見ると必須は **AWS dev E2E だけ**、Anywhere は「必須ではないが強く推奨の中間ゲート」。
- 両者の検証範囲は**入れ子**：`AWS dev E2E ⊇ Anywhere`。Anywhere が見る「GameLift SDK 契約」は Managed EC2（＝dev）でも同じコードが動くので、dev の**部分集合**。カバレッジ上は dev だけで足りる。
- Anywhere の価値＝**同じ SDK 検証を課金前に無料枠で先に潰す**コスト/リスク最適化。GameLift フリートは「立てているだけで時間課金」される最大コスト源なので、そこに入る前に無料で SDK を通す保険は費用対効果が高い。
- いきなり dev（Managed EC2）直行も**カバレッジ的には成立**するが、SDK 契約のデバッグを課金中のフリート上で回すことになる（遅い・高い）。
- 帰結：local-first・$200 Budget 方針なら **Anywhere を挟む**のが整合的。手間最小化優先なら Anywhere スキップも選択肢。

### Q. Anywhere と AWS dev は、どちらも「GameLift のサービス」という立ち位置?
**違う。カテゴリが別。**
- **GameLift Anywhere → GameLift の中の一機能**（フリート種別のひとつ）。**Yes、GameLift のサービス。**
- **AWS dev → GameLift の機能ではなく「環境（environment）」という別軸**。Cognito・API GW・Lambda・DynamoDB・GameLift・Route53… **全部入りの箱**の名前。GameLift はその**一部品**にすぎない。**No、GameLift のサービスではない。**
- .NET 例え：Anywhere=`IHostingProvider` の実装のひとつ（`AnywhereProvider`）／dev=`appsettings.Development.json` の「環境」。レイヤーが違う。
- 2軸で整理：

| | 環境=ローカル寄り | 環境=AWS dev |
|---|---|---|
| フリート種別=Anywhere | ← 今回の「Anywhere 実接続」（自 PC が compute） | 通常やらない |
| フリート種別=Managed EC2 | 不可（EC2 は AWS 上） | ← 「AWS dev E2E」（本番同等） |

### Q. Anywhere と Managed EC2 の違いは「compute を誰が用意するか」だけ?
**違う。それは氷山の一角。** 本質は **Managed EC2=GameLift が compute のライフサイクルを丸ごと管理／Anywhere=GameLift が管理するのは制御プレーン（セッション配置・マッチメイキング・SDK 契約）だけで、compute 周りは全部あなたの責任**。

| 観点 | Managed EC2 | Anywhere |
|------|-------------|----------|
| compute 調達 | GameLift が EC2 自動プロビジョン | 自分で用意・登録（`RegisterCompute`） |
| Build 配布 | zip アップロードで各 EC2 に**自動配置** | **アップロード無し**。バイナリを自分で置いて起動 |
| オートスケール | 需要で**自動増減** | ハードのスケール無し。台数は自分で管理 |
| Spot/コスト | **Spot で約70%減**・時間課金 | 自前ハード（自 PC ≒無料）。GameLift 課金は極小 |
| OS/パッチ/ヘルス | GameLift が管理・不健全機を**自動置換** | 自分で管理。GameLift は unhealthy 判定のみ・ハード置換なし |
| ネットワーク | SG/inbound・TLS 証明書を管理 | 自分で IP・ポート開放・到達性 |
| 可用性 | AZ 分散・冗長を GameLift が担保 | 自 PC が落ちたら終わり |

- **共通は「制御プレーン」だけ**：どちらも SDK ライフサイクルは同一 → Anywhere で通せば Managed EC2 でも通る（＝入れ子の根拠）。
- .NET 例え：Managed EC2=Azure App Service（PaaS 丸投げ）／Anywhere=自前サーバーに自分でデプロイしつつ司令塔だけ GameLift に委任するハイブリッド。

### Q. Anywhere は「ローカルサーバーのため」より「本番想定 GameLift が問題なくデプロイ・機能するかのテスト」のため?
**方向性は正しい**（＝ローカルでゲームを動かすこと自体が目的ではなく、本番で使う GameLift と自サーバーの統合検証が目的）。ただし**「デプロイ」まで含めると不正確**。
- Anywhere が検証するのは **SDK 契約＝制御プレーンとの会話**。**Managed EC2 のデプロイ経路そのものは検証しない**。

| 本番 GameLift の要素 | Anywhere で検証 | 
|---|---|
| SDK ライフサイクル（InitSDK→ProcessReady→OnStartGameSession→AcceptPlayerSession→RemovePlayerSession→ProcessEnding）/ヘルスチェック/マッチ→配置結線 | ✅ できる（本命） |
| Build アップロード＆自動配置 | ❌ できない（自 PC で直接起動、zip 経路なし） |
| Fleet プロビジョニング（EC2 調達）/オートスケール/Spot 回収/OS・AZ 冗長 | ❌ できない（dev の担当） |

- 正確な言い方：**「本番 GameLift が“機能する”か＝Yes（SDK 契約の部分）／“デプロイ”できるか＝それは AWS dev（Managed EC2）で初めて検証」**。
```
Anywhere    : サーバーコード ↔ GameLift SDK契約（デプロイ経路は含まない）
AWS dev E2E : 上記 ＋ Build/Fleet/EC2 デプロイ経路 ＋ 全マネージドサービス統合
```

### Q. ローカルのセッション作成 Lambda が実 GameLift 疎通と違い、playerId でなく playerSessionId で判断が必要＝そのために作る、で合ってる?
**大筋合っているが 2 点補正。** 指摘には**軸の違う2つのギャップ**が混ざっている。両方 Anywhere に要るが担当が別。

| 違い | 内容 | ギャップ |
|---|---|---|
| セッション作成の**経路**が違う | ローカルは `GameLiftStub`（固定接続先）+ `hubSessionProvider` が roster を同居 Hub に直接 Register。実 GameLift は Lambda が `CreateGameSession`→GameLift が配置→サーバの `OnStartGameSession` 発火 | **G2** |
| 識別子の**判断**が違う | roster 判定を `playerId` でやっているが GameLift の受け入れは `playerSessionId` | **G1** |

**補正1**：「セッション作成 Lambda が実疎通と異なる」＝G2、「playerId でなく playerSessionId」＝G1。別の話。今回の G1-min は後者（識別子の配線）が対象、Lambda の実 CreateGameSession 化（G2）は別途。

**補正2**：playerId を playerSessionId に**置き換えるのではない**。**playerSessionId の受け入れ層を“追加”し、対応表で playerId と結ぶ**（両者は役割が違い共存）。
```
playerSessionId … GameLift がコネクション毎に発行する入場チケット
                  → AcceptPlayerSession(psid) で検証 / RemovePlayerSession(psid) で返却（GameLift 契約レイヤ）
playerId        … ゲームドメイン identity。roster 判定・ゲーム状態・履歴保存はこのまま（アプリロジックレイヤ）
対応表 psid→playerId … 2 層を橋渡し（G1 で新設）
```
- 具体：クライアントは接続時に playerSessionId を提示 → サーバが `AcceptPlayerSession(psid)` で GameLift に検証させ、対応表から playerId を確定 → 以降のゲーム処理は従来通り playerId。`session.go` の roster(playerId) 判定は**残し**、その手前に「psid を受理して playerId に解決する段」を**足す**。
- 帰結：**G1=playerSessionId 受け入れ層を足し playerId と繋ぐ／G2=セッション作成を実 GameLift に差替**の二本立て。G1-min は前者をローカル no-op 込みで配線し、実 GameLift 成功確認は G2 と同時に後段。

### Q. なぜ playerId と playerSessionId が 1:1 になる? マッチ→同一セッション接続までの流れは?
**playerSessionId は「そのセッションにおける、その人の座席予約」**だから。1 セッションに 4 人いれば座席予約も 4 つ、各予約はちょうど 1 人に対応 → **同一セッション内で playerId ↔ psid は 1:1**。

**流れ（4人がマッチして同一セッションへ）**:
```
各自 login → playerId = JWT sub（人ごと固有・不変）
各自 match/request → ticket（人ごと）
マッチ成立（4枚を1グループ）→ Lambda finalize:
  (1) CreateGameSession(questId,[p1..p4]) → 【1つ】の gameSessionId ＋ IP:Port  ★全員共通
  (2) CreatePlayerSession(GS, playerId) を人数分 → psid_1..psid_4              ★人ごと=ここで1:1が生まれる
各自 match/status で受取：IP:Port（共通）＋ 本人の psid
全員が同じ IP:Port へ UDP 接続 → 各自 psid を提示 → server.AcceptPlayerSession(psid) で座席確定
```

**1:1 が生まれる場所**: (2) の `CreatePlayerSession` を「1 人 1 回」呼ぶから。1 セッション内に人数分の座席ができ、各 psid はちょうど 1 人分の枠を指す。時間をまたぐと同じ playerId が別セッションで別 psid をもらう＝通時的には 1:多。**「1 セッション内でだけ 1:1」**が正確。

**飛行機の例え**:
| GameLift | 飛行機 | 性質 |
|---|---|---|
| playerId(JWT sub) | パスポート番号 | 人に固有・不変。どの便でも同じ |
| gameSessionId＋IP:Port | 便名＋搭乗ゲート | その便の全員で共通 |
| playerSessionId | 搭乗券＋座席番号 | 1便1人1枚。次の便では別の券 |
| AcceptPlayerSession(psid) | ゲートで搭乗券スキャン | 提示券が本物か検証 |

**ユーザーごと（4人なら4つ）**: playerId／ticket／**playerSessionId(psid)**／match/status で配る psid 部分。
**セッション共通（1つ）**: gameSessionId／**接続先 IP:Port**／questId・roster／サーバープロセス（1セッション=1プロセス）／Sessions レコード。

**なぜ共通 IP:Port だけでは足りず psid が要るか**: 全員が同じ IP:Port に来るので、サーバーは「今 UDP してきた接続が誰で正規参加者か」を判別する手段が必要。playerId 自己申告は詐称可能。GameLift 発行の psid を `AcceptPlayerSession` で検証してこそ「正規に予約された座席の持ち主だけ」を通せる。→ マッチャー側(供給=人数分 psid 配布)とサーバー側(検証=AcceptPlayerSession)が **供給1つ:検証1つ** で対応する必要がある（= per-player psid 実装 [[project_phase4_anywhere_plan]] G1/G2 の接点）。

### Q. goroutine と channel の違いは?（C#/.NET 視点）
混同しやすいが役割は別物。**goroutine = 並行に走る実行の流れ（働く人）**、**channel = goroutine 間でデータを受け渡す型付きパイプ（人と人をつなぐ通信路）**。channel は「実行グループ」ではない。
- **goroutine**: 関数の前に `go` を付けるだけで別の流れで動き出し、呼び出し元はブロックされず次へ進む（C# の `Task.Run(...)` を await しない感覚）。OS スレッド（C# `Thread` ≒ 1MB/本）と違い初期スタック ~2KB で、Go ランタイムが少数の OS スレッド上に大量の goroutine を M:N 多重化する（.NET の `ThreadPool`+`async/await` に思想が近い）。Go はコードを**見た目同期的**に書くだけで I/O ブロック時にランタイムが裏でスレッドを融通するため、`async`/`await` キーワードが存在しない（function coloring が起きない）。
- **channel**: 何本でも独立に作れ、`<-ch` で受信。重要ルール「**`<-ch` はその行を書いた場所を実行している goroutine を待機させる**」。待つ主体は常に goroutine、channel は待たれる対象。
- **`select`**: 複数 channel を**同時に見張り**、最初に動いた 1 本で進む構文（C# の `Task.WhenAny` 相当。単発 `<-ch` が `await task`）。1 goroutine が複数 channel を順に `<-` すると直列待ちになるので、同時待ちは `select`。
- **main goroutine の特例**: `main()` も 1 つの goroutine。**main が return すると走行中の他 goroutine ごとプロセスが即終了**する。だからサーバを `go` で裏に回したら main を別途待機させないと、起動直後に終わってしまう。

### Q. `localapi` の goroutine / channel 構成は?（グレースフルシャットダウン）
`main.go` は **3 系統の goroutine** を、**1 つの context キャンセルを起点に**自律的に畳む構成。
```
main goroutine            … <-shutdownCtx.Done() で待機（OS シグナルの番人）
├ goroutine A: go srv.Serve(ctx)        … UDP 受信ループ（gameserver.go:52）
│   └ goroutine A': go broadcastLoop(ctx)  … 50ms 周期で状態配信（Serve がさらに産む）
└ goroutine B: go func(){ ListenAndServe }()  … HTTP（main.go:101）
```
- **channel の中心は `signal.NotifyContext`**（main.go:56）。内部で OS シグナル用 channel を作り、SIGINT(Ctrl+C)/SIGTERM で `shutdownCtx` をキャンセル。context のキャンセルは内部的に **`Done()` が返す channel を close** することで表現される（C# の `CancellationToken`、`Done()`≒`token.WaitHandle`）。
- **`<-shutdownCtx.Done()`（main.go:108）が待たせるのは main goroutine**、待つ対象は **OS シグナル**（サーバ処理でも他 goroutine でもない）。ここで main を眠らせておくのが、上記「main が終わると全部死ぬ」への対処＝**停止処理を正常に走らせるための門**。
- **HTTP と UDP の停止は連鎖ではなく独立**。同じ `shutdownCtx` キャンセルを各自が受けて別々に止まる：UDP は `Serve(ctx)` が `ctx.Done()` を見て**自律停止**、HTTP は `ctx` を見ないので**main が目覚めてから `srv.Shutdown(timeout)` で明示停止**（処理中リクエストをドレイン）。`ListenAndServe` は正常停止時 `http.ErrServerClosed` を返すので、それだけは正常系として無視する。
- 「1 つの close を、見ている全員が同時に受け取る」= **close をブロードキャストに使う**Go 定番パターン。グレースフルシャットダウンはその典型。

### Q. UDP の `receiveLoop` はなぜ `select` だけで止められない?
**ソケットの read（`ReadFromUDP`）は channel ではないので `select` に混ぜられない**ため。2 つのループで停止検知の作りが違う。
- **broadcastLoop（教科書どおりの select）**: `select { case <-ctx.Done(): return; case <-ticker.C: Tick() }`。channel 2 本（`ctx.Done()` とタイマー `ticker.C`）の**ブロッキング同時待ち**。停止は最大 50ms 以内に検知。
- **receiveLoop（締切ポーリング）**: 先頭の `select` は `default` 付き＝**ノンブロッキング**で「`ctx.Done()` が**もう**閉じてるか」だけ即チェック（C# の `token.IsCancellationRequested` 相当）。本体の `ReadFromUDP` はパケットが来るまでブロックするので、`SetReadDeadline(500ms)` で締切を付け、**最大 500ms ごとに必ずループ先頭へ戻して** `ctx` を再チェックする。タイムアウトエラーは `ne.Timeout()` で握りつぶして `continue`（＝ctx を見直す口実）。締切が無いと受信行で固まり、停止指示に気づけない。
- 一般化：**channel は `select` で待てるが、ソケット read は待てない。だから締切でポーリング周期を作り、その隙間で ctx を覗く**——ネットワークサーバ頻出のパターン。

### Q. `localapi` の実行方法は?
依存（DynamoDB=LocalStack / JWT 発行=mock-auth）が要るので**単体では動かない**。手順の正は `docs/phase4-e2e-runbook.md`。要点:
```powershell
# 1) 依存スタック起動（DynamoDB テーブルは init が自動作成）
cd C:\work\RAIDYNC
docker compose -f docker/docker-compose.yml up -d
# 2) ビルド
cd C:\work\RAIDYNC\server
go build -o ..\bin\localapi.exe .\cmd\localapi
# 3) 環境変数をセットして起動（このターミナルは起動しっぱなし＝<-shutdownCtx.Done() で待機する状態）
cd C:\work\RAIDYNC
$env:DYNAMODB_ENDPOINT="http://localhost:4566"; $env:AWS_REGION="ap-northeast-1"
$env:AWS_ACCESS_KEY_ID="test"; $env:AWS_SECRET_ACCESS_KEY="test"   # LocalStack 用ダミー
$env:AUTH_JWKS_URL="http://localhost:9229/.well-known/jwks.json"
$env:AUTH_ISSUER="http://localhost:9229"; $env:AUTH_CLIENT_ID="raidync-local-client"
.\bin\localapi.exe
```
- 起動成功ログ＝`gameserver(udp) listening on 127.0.0.1:7777` と `listening on :8080`（前述 2 系統の goroutine）。
- 停止は **Ctrl+C**（SIGINT→`signal.NotifyContext`→`shutdownCtx` キャンセル→3 goroutine が自律的に畳まれグレースフル終了）。
- AppControl(WDAC) で `.exe` が弾かれたら `server` ディレクトリから `go run .\cmd\localapi`（同じ環境変数を設定の上）。E2E 完走は別ウィンドウで `client.exe` を実行し `client: E2E completed` を確認。

### Q. GameLift Anywhere Runbook（phase4-anywhere-runbook.md）は AWS の GUI 操作でも行える?
**一部のみ可能。中核ステップは実質 CLI/SDK 専用**で、GUI だけでは完結しない。
- **GUI で代替可**: ①`create-location`（カスタムロケーション）と ②`create-fleet`（Anywhere フリート）は GameLift コンソールの「Create fleet」ウィザードから作成できる。
- **CLI/SDK が必須**: ③`register-compute`（コンソールは実行コマンドを案内表示するだけで GUI 登録フォームは無い。返り値 `GameLiftServiceSdkEndpoint` は API で取得）、④`get-compute-auth-token`（**短命トークン**・起動ごとに取り直し＝GUI 非現実的）、⑥`create-game-session`（コンソールに手動起動ボタン無し）。⑤は自 PC でのプロセス起動で AWS 操作ではない。
- **結論**: フリート/ロケーション作成だけは GUI で一度作って構造把握するのはアリ。ただし Runbook 全体の GUI 置換は不可。狙いは「動いた構成を後で Terraform 化」（IaC 徹底）なので、手作業 GUI より CLI のままの方が移植しやすい。

### Q. psid の「上り」配管とは? G1-full で何を通した?
psid には **下り（配布）** と **上り（提示→検証）** の 2 経路があり、G1-full は空白だった上りを埋めた。

**下り（マッチ API → クライアント）＝ 既に開通済み**:
```
Lambda finalize: CreateGameSession + CreatePlayerSessions で psid 生成
  → ticket.ConnectionInfo.PlayerSessionID に格納（人ごと）
  → GET /match/status で各クライアントへ配布
```
ここに **UDP ゲームサーバは一切登場しない**（psid は REST の制御プレーンだけを通る）。

**上り（クライアント → ゲームサーバ → GameLift）＝ G1-full で開通**:
```
client sendUDP: 受け取った psid を毎パケットの GameMessage.PlayerSessionID に載せて提示
  → server.Ingest: 参加時（最初のパケット）に提示 psid を採用し対応表へ書き戻し
  → OnPlayerJoin(psid) → observer → （実 AWS）AcceptPlayerSession(psid) で座席承認
```

**なぜ一度クライアントに戻すのか**: ①Lambda は短命 HTTP でリアルタイム UDP を中継できない、②ゲーム接続は NAT 内のクライアント発でしか張れない、③マッチ API とゲームサーバは直接会話しない（両者を繋ぐのはクライアントだけ）。飛行機の搭乗券モデルそのもの＝代理店(制御)が券を発行し、本人が持ち歩き、ゲート(データ)で提示・照合。[[Q. なぜ playerId と playerSessionId が 1:1 になる?]] の続き。

**実装（3 ステップ）**: (1) `protocol.GameMessage` に `PlayerSessionID` 追加（毎パケット提示・採用は参加時のみ＝UDP で JOIN 欠落しても後続で再確立）、(2) `cmd/client.sendUDP` が psid を提示、(3) `cmd/localapi` に `loggingObserver` を注入し psid の到達をローカル可視化。**新メッセージ種別は足さず既存の暗黙参加モデルに 1 フィールドで載せた**。

### Q. observer と WithPlayerSessions は何をする? ローカル observer は実 AWS で何?
- **PlayerObserver**: セッションの参加/離脱を外部へ通知する口（C# の interface コールバック/event 相当）。`Session` は宛先を知らず「入った/出た」と叫ぶだけ＝依存性の逆転。実装を差し替えるだけで振る舞いが変わる。
- **WithPlayerSessions**: `playerId→psid` 対応表の初期値注入（関数型オプション）。G1-full で位置づけ格下げ＝**提示 psid が優先、これは fallback**。実フローでは psid がセッション作成後発行で GameProperties に載らないため、対応表は空で始まりクライアント提示で埋まる。
- **ローカル軽量 observer ＝ 実 AWS の `GameLiftBridge` の代役**。同じ `PlayerObserver` の別実装で、ローカルはログ出力（No-op）、本番は `OnPlayerJoin→AcceptPlayerSession(psid)` / `OnPlayerLeave→RemovePlayerSession(psid)` を実 GameLift へ送信。**Session 側コードは同一**、G4 では「ログ実装 → Bridge」に挿し替わるだけ。

### Q. Anywhere で作った G1-full は実 GameLift(Managed EC2)へ移すとき作り直しになる?
**ならない。** Anywhere と Managed EC2 は**フリート種別が違うだけで、ゲームサーバが使う Server SDK 契約（InitSDK→ProcessReady→OnStartGameSession→AcceptPlayerSession→RemovePlayerSession→ProcessEnding）は完全同一**。G1-full が触るのは全部この層なので Anywhere で通れば Managed EC2 でもそのまま動く。違いは compute 提供元・デプロイ方法・SDK 認証情報の入手経路（Anywhere は手で env 設定 / Managed EC2 は GameLift が自動注入、ただし読む側は同じ `InitSDKFromEnvironment`）で、いずれもコード外。唯一分岐する管理側 `CreateGameSession` の Location は G2 の `locationPtr()` で吸収済み。**「1 回書けば Anywhere でも AWS dev でも使える」**投資。

### 実装メモ（Phase 4 G1-full：psid 上り配管 完了・2026-07-13）
`GameMessage.PlayerSessionID` 追加 → `session.Ingest` で提示 psid 採用・書き戻し → `cmd/client` 提示 → `cmd/localapi` に `loggingObserver` 注入。ローカル E2E（LocalStack + mock-auth + localapi + client）で `gameserver: player join ... psid=psess-...` / `player leave ...（同一 psid）` を実パケット往復で確認。psid が「発行→配布→提示→採用→observer 到達」まで一気通貫。残り [[project_phase4_anywhere_plan]]: G4（実 Anywhere で observer→Bridge に替え AcceptPlayerSession 承認）／G3（Terraform 化）／AWS dev E2E。

### Q. G4（Anywhere Runbook）着手に必要な AWS 認証項目は?
「認証が通る」＝ `aws sts get-caller-identity` が成功する状態（CLI が資格情報を解決し IAM プリンシパルを確定）。必要項目は4つ: **アクセスキー ID / シークレットアクセスキー / リージョン（`ap-northeast-1`）/ 出力形式（`json`）**。`aws configure` で `%USERPROFILE%\.aws\` に保存。
- **資格情報の入手**: (A) IAM ユーザーのアクセスキー発行→`aws configure`（個人ポートフォリオはこれで十分・推奨）、(B) IAM Identity Center `aws sso login`（一時資格情報・失効あり・組織向け）。
- **必要な IAM 権限**: `gamelift:` の CreateLocation/CreateFleet/DescribeFleetAttributes/RegisterCompute/GetComputeAuthToken/CreateGameSession/CreatePlayerSessions/DeleteFleet/DeleteLocation ＋ `sts:GetCallerIdentity`。runbook 前提の `gamelift:*` で全カバー。専用 IAM ユーザーにインラインポリシー1本で足りる。
- **重要**: runtime（ステップ5 の gameserver 実行）は IAM 資格情報を**使わない**。SDK 接続は `GAMELIFT_SDK_AUTH_TOKEN`（ステップ4 取得の短命トークン）で行う。IAM 資格情報が要るのは **ステップ1〜4・6 の `aws gamelift ...` を叩く CLI 側だけ**。
- **セキュリティ/コスト**: アクセスキーはリポジトリにコミットしない（`.aws/` はリポジトリ外）。検証後はフリート/ロケーションを削除（月 $200 制約）。

### Q. GameLift を GUI でも操作したい。ログイン主体はルートでいい?
**ダメ。GUI 操作専用の IAM ユーザー（`raidync-admin`）を作る。** CLI 用の `raidync-cli` はコンソールアクセス無しで作ったため GUI ログイン不可。かといってルートで日常 GUI 操作するのは「ルート封印」方針に反する。よって責務分離で 3 層に:
- **ルート**: 封印（請求・アカウント設定などルート専用のみ）。
- **`raidync-admin`**: 人間の GUI 操作用。コンソール+パスワード+**MFA**、**長期キー無し**、AdministratorAccess（人間管理者は広め＋MFA 保護／サービス実行・IaC はロールで最小権限、という設計）。
- **`raidync-cli`**: 自動化/CLI 用。アクセスキーのみ・最小権限スコープ。

**作成は初回だけルートでコンソールログインして GUI で行う**（ユーザー作成に IAM 書き込み権限が要るが `raidync-cli` は IAM 権限を持たず、CLI 作成にはルートキーが要る＝封印方針に反するため）。手順は docs/aws-iam-user-setup.md「GUI 用 IAM ユーザー」節。

### Q. GameLift を呼んだら `SubscriptionRequiredException`（GUI では "Complete your account setup / currently on free plan"）。原因と対処は?
**AWS アカウント自体の本アクティベーション未完了**が原因。IAM 権限やポリシーの問題ではない（`sts:GetCallerIdentity` は通るのに GameLift だけ弾かれる）。GameLift のような従量課金サービスは、アカウントが完全有効化されるまで GUI/CLI どちらからも呼べない。
- **対処（ルートで）**: ①支払い方法（クレカ）登録、②本人確認（電話/SMS・住所）、③サポートプラン選択（**Basic=無料**でOK）、④AWS からのメール依頼に応答（迷惑メール確認）、⑤"free plan" 表示なら**有料（従量課金）プランへ切替**。これらは請求/アカウント設定＝**正当なルート用途**（封印方針に反しない）。
- **待ち**: カード検証で $1 前後の仮押さえ（3〜5 日・認証のみ）、サービス全有効化まで**最大 24 時間**。
- **課金の心配**: 有料プラン切替＝即課金ではない。AWS は従量課金なので未使用なら $0。予算アラート $200/月 も別途。
- **判定**: `aws gamelift list-locations --region ap-northeast-1` が `SubscriptionRequiredException` を返さなくなれば有効化完了→ステップ1（create-location）へ。

---

## 12. Phase 4：AWS dev E2E（本番同等環境）

### Q. terraform は本番想定と同等の認識。その上で GameLift Anywhere の Terraform 化は必要?
**本番構成としては不要。** Anywhere は「自 PC を compute にする**開発時の検証手段**」であって本番の部品ではない（本番は Managed EC2 + Spot）。§11 の2軸表で言えば Anywhere は「環境=ローカル寄り」側に属し、「環境=AWS dev × フリート種別=Anywhere」は *通常やらない* セルにあたる。
- **ただしコードを捨てるのは別問題**：G3 の成果であり、Anywhere 疎通をやり直す際の再現性、「検証手段まで IaC 化した」証跡としての価値がある。→ **捨てずに置き場所を直す**のが正解。
- **対応（2026-07-22）**：`environments/dev` から **`environments/anywhere`** へ分離。モジュールも `modules/gamelift` → **`modules/gamelift_anywhere`** にリネーム（ステップ6 で作る Managed EC2 用 `gamelift_ec2` との名前衝突・混同を避けるため）。
- **分離の実益**：①`dev` が「本番同等」を名乗れる（開発専用リソースが混在しない）②`GAMELIFT_FLEET_ID` をどちらのフリートに向けるかの曖昧さが消える（E2E で使うのは Managed EC2 側のみ）③**state 分離**により dev の `apply`/`destroy` サイクルに無関係な検証用フリートを巻き込まない。
- **タイミングの妙**：まだ一度も `apply` しておらず state が空だったため移動コストはゼロ。後からなら `terraform state mv` が必要だった。

### Q. AWS dev E2E に VPC / Route53 は要る?
**最小構成では不要**（意図的にスコープ外）。
- **VPC/NAT 不要**：Lambda は DynamoDB・GameLift を**パブリックエンドポイント**で呼べる。VPC に入れると外部通信に NAT Gateway が要り、**時間課金（概算 ~$30/月）**が発生する。dev の目的（統合検証）に VPC は寄与しないため置かない。
- **Route53 不要**：API Gateway の実行 URL を直叩き、GameLift は IP 直接接続で E2E は成立する。レイテンシールーティングは Phase 5（マルチリージョン）の担当。ホストゾーンは月額固定課金（概算 $0.50/ゾーン）。
- 方針の一貫性：「**立てているだけで課金されるもの**を dev に置かない」。唯一の例外が GameLift Managed EC2 フリート（E2E に必須）で、これは検証後に必ず destroy する。

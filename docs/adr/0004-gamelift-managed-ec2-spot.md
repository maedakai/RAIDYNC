# ADR-0004: ゲームサーバーは GameLift Managed EC2 で動かし、Spot をプライマリにする

- **ステータス:** 採用（On-Demand へのフォールバックとオートスケールは未実装）
- **日付:** 2026-07（Phase 4）
- **関連:** [ADR-0003](0003-udp-with-seq-gate.md)、[ADR-0005](0005-no-vpc.md)、`terraform/modules/gamelift_ec2`、`terraform/modules/gamelift_anywhere`、`server/internal/matchmaking/gamelift_provider.go`

## 背景

ゲームサーバーは、4 人と最大 20 分間 UDP でつながり続け、その間セッションの状態をメモリに持つ（[ADR-0003](0003-udp-with-seq-gate.md)）。この性質は Lambda には載らないので、常駐するサーバープロセスが要る。

置き場所を選ぶうえで鍵になるのは、**スケールの指標が CPU ではない**ことだった。

- 汎用のコンテナ基盤（ECS、EKS）は、CPU 使用率やリクエスト数でスケールする
- ゲームサーバーで見るべきなのは「空いているセッションの枠」の数。CPU が 30% でも 4 人で埋まっていれば新しいセッションは入らず、CPU が 80% でも空きがあれば入る。CPU と収容能力が一致しない

汎用基盤を選ぶと、次の 4 つを自分で作ることになる。

1. **セッションの配置:** 新しいマッチを、どの空いたサーバーに割り当てるか
2. **ドレイン:** スケールインのとき、進行中のセッションを殺さずに新規の受付だけ止め、終わるのを待つ
3. **Spot の中断への対応:** 2 分前の中断通知を受けて、セッションを退避させる
4. **マッチメイキング**

## 検討した選択肢

| | 案 | 評価 |
|---|----|------|
| A | ECS Fargate / EKS に UDP のコンテナを置き、NLB で受ける | 不採用 |
| B | EC2 の Auto Scaling グループに自前で配置する | 不採用 |
| C | **Amazon GameLift Managed EC2** | 採用 |
| D | GameLift Anywhere（自分で用意したマシンを GameLift に登録する） | 開発時の検証だけに使う |

- **A / B**: 上の 1〜3 を自分で作る必要がある。インフラの部品としては身近だが、作るものの大半がゲームサーバー特有の運用になる
- **C**: 配置、ドレイン（セッション保護）、ゲームセッションとプレイヤーセッションの管理、空き枠を指標にしたスケールを GameLift が持っている
- **D**: compute を誰が用意するかだけが C と違い、ゲームサーバーのコードと SDK の使い方は同じ。自分の PC を compute にすれば、GameLift Server SDK 5.x との接続をほぼ無料で確かめられる。SDK 5.x では、以前のローカル用エミュレーター（GameLift Local）が使えなくなったので、その代わりになる

## 決定

**案 C を本番の構成とし、フリートは Spot をプライマリにする。** Anywhere（D）は開発時の検証手段とし、Terraform の state を `environments/anywhere` に分けて、dev の apply / destroy に巻き込まないようにする。

- **Spot をプライマリにする理由:** On-Demand より約 70% 安い。ゲームサーバーは 1 セッションが最大 20 分で終わる短命な処理で、中断されても失うのはそのセッションだけになる
- **On-Demand をフォールバックとして残す理由:** Spot の中断通知は 2 分前で、20 分のセッションを逃がしきるには足りない。Spot の在庫が尽きたときに、新しいセッションを受けられなくなるのも避けたい
- **GameLift Build は作り直す運用にする。** Build は更新できないリソースなので、zip の md5 を S3 のキーと Build の `version` に入れ、中身が変わったら作り直させる
- **フリートは使うときだけ作る。** このプロジェクトで唯一、置いているだけで時間課金されるリソースなので、`enable_gamelift`（既定 `false`）で E2E のときだけ両リージョンに作り、終わったら消す
- **FlexMatch は使わない。** マッチメイキングは自分で作る（[ADR-0001](0001-optimistic-lock-matchmaking.md)）。Lambda から `CreateGameSession` と `CreatePlayerSessions` を直接呼び、参加者の一覧とクエスト ID は `GameProperties` で渡す

## 結果

### 良い点

- 配置、セッションの起動（`OnStartGameSession` → `ActivateGameSession`）、プロセスの終了と再利用（`ProcessEnding`）を GameLift に任せられた。ゲームサーバーのコードは、ゲームの処理と SDK の呼び出しに集中している
- AWS dev で、実際の Cognito → API Gateway → Lambda → GameLift（Spot、c5.large）→ UDP の経路を通して確かめた
- 同じゲームサーバーのバイナリが、Anywhere・Managed EC2・ローカルの同居ハーネスで動く

### 悪い点・残るリスク

- **On-Demand へのフォールバックはまだない。** 今は Lambda が Spot フリートの ID を直接指定して `CreateGameSession` を呼んでいる。フォールバックするには、Spot と On-Demand のフリートを並べたゲームセッションキューを作り、キューに配置を頼む形（`StartGameSessionPlacement`）に変える必要がある。その場合、配置が非同期になるので、マッチ成立の処理も配置の完了を待つ形に変わる
- **オートスケールは見送った。** 確かめるには、負荷をかけたまま複数台を動かし続ける必要があり、費用がかかるため。フリートの台数は Terraform の対象外（`UpdateFleetCapacity` の API の領分）で、今は既定の 1 台のまま
- **1 台で 1 セッションしか動かさない。** ゲームサーバーが固定ポート 7777 で待ち受けるため、`concurrent_executions = 1` にしている。1,000 セッションには 1,000 台が要る計算になる。ポートをプロセスごとに変えて 1 台に複数のプロセスを載せれば、台数は大きく減らせる
- **dev では進行中のセッションを保護していない**（`NoProtection`）。Spot の中断やフリートの整理を妨げないための設定で、本番では `FullProtection` にしてドレインさせる
- **Build の作成は awscc provider に頼っている。** classic の aws provider に `server_sdk_version` がなく、AL2023 と SDK 5.x の Build を作れないため。awscc は CloudFormation の Cloud Control API を経由するので、デプロイ用の IAM 権限に `cloudformation:*Resource` が要る

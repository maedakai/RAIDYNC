# ADR-0008: トレースは OpenTelemetry で計測し、X-Ray の OTLP エンドポイントへ直接送る

- **ステータス:** 採用
- **日付:** 2026-10-18（OpenTelemetry の採用）、2026-10-27（直接送る方式の採用）、2026-11-06（伝播の形式）。Phase 6
- **関連:** [ADR-0007](0007-emf-metrics.md)、`server/internal/telemetry`、`server/internal/db/client.go`

## 背景

RAIDYNC の 1 リクエストは、API Gateway → authorizer → Lambda → DynamoDB（複数回）→ GameLift と何段も通る。メトリクスは全体の集計で、ログは部品ごとの自己申告なので、どちらを見ても「この 1 回が、どこで何ミリ秒使ったか」は分からない。負荷テストで応答の p95 が悪化したとき、原因がコールドスタートなのか、DynamoDB の競合なのか、GameLift の API なのかを切り分けたかった。

計画では X-Ray を使う想定だった。調べると次のことが分かった。

- Go 用の X-Ray SDK は 2026-02-25 にメンテナンスモードに入り、2027-02-25 にサポートが終わる。AWS は代わりに OpenTelemetry（OTel）を勧めている
- LocalStack の無料版は X-Ray に対応していないので、X-Ray だけに頼ると AWS にデプロイしないとトレースを見られない

## 検討した選択肢

### 1. 計測の仕組み

| | 案 | 評価 |
|---|----|------|
| A | X-Ray SDK for Go | 不採用（サポート終了が決まっている） |
| B | Terraform だけで API Gateway と Lambda のトレースを有効にする（コードは計測しない） | 不採用 |
| C | **OpenTelemetry でコードを計測する** | 採用 |

- **B**: 各段の時間とコールドスタートは見えるが、Lambda の中は 1 つの塊になり、DynamoDB の呼び出しごとの時間は見えない
- **C**: 送り先を選べるので、ローカルでは Jaeger、AWS では X-Ray に送れる。Datadog や Grafana でも使える業界標準で、知識を使い回せる

### 2. AWS での送り方

| | 案 | 評価 |
|---|----|------|
| A | Lambda レイヤーで Collector（ADOT）を同居させ、アプリは `localhost:4317` に送る | 不採用 |
| B | **アプリが X-Ray の OTLP エンドポイントへ、SigV4 で署名して直接送る** | 採用 |

- **A**: アプリのコードはローカルと同じ形（gRPC で `localhost:4317` に送る）のままで済み、署名も Collector がやってくれる。ただし Collector は別プロセスなので、コールドスタートが伸び、メモリも使う。負荷テストで測る p95 に、計測の仕組み自体の遅れが混ざってしまう
- **B**: 余分なプロセスがない。AWS が今勧めている方式でもある。代わりに、エンドポイントは HTTP だけ（gRPC 不可）で、SigV4 署名を自分で付ける必要がある。また CloudWatch Transaction Search を有効にする必要がある

## 決定

**OpenTelemetry で計測し（1-C）、AWS では X-Ray の OTLP エンドポイントへ直接送る（2-B）。**

- **計測はハンドラーのコードに書かない。** HTTP（`otelhttp`）、Lambda（`otellambda`）、AWS SDK（`otelaws`）の計測ライブラリを、入口と `db.NewClient` の 1 か所ずつに入れるだけにした。コードはもともと全体で `ctx` を引き回していたので、OTel で一番手間のかかる Context の受け渡しは済んでいた
- **送り先は環境変数 `TRACE_EXPORTER` で切り替える。** `xray` なら、SigV4 署名を付ける `http.RoundTripper` を挟んだ OTLP/HTTP の Exporter を使う。それ以外ならローカル用の OTLP/gRPC（Jaeger）を使う。どちらを選んだかは起動時にログに出す（環境変数を渡し忘れたら、Span は黙って消えるため）。GameLift の本物とスタブを環境変数で切り替える既存の `selectSessionProvider` と、同じ形にそろえた
- **API Gateway と Lambda のトレースをつなぐ。** API Gateway は X-Ray 形式の ID（`X-Amzn-Trace-Id`）を渡し、OTel の既定は W3C 形式（`traceparent`）なので、そのままだとトレースが 2 本に分かれる。伝播の形式を W3C と X-Ray の両方を読める組み合わせ（`CompositeTextMapPropagator`）にした。X-Ray 形式だけに置き換えると、ローカルや将来のクライアントが W3C 形式で送ってきたときに読めなくなるため
- **AWS 側の準備はすべて Terraform で書く。** Transaction Search の有効化（`awscc_xray_transaction_search_config`）、X-Ray がログを書くためのリソースポリシー、Lambda の `xray:PutSpans` 権限、API Gateway と Lambda のトレース有効化、負荷テスト用のサンプリングルール（変数で全件に切り替えられる）

## 結果

### 良い点

- ローカルファーストの方針のままトレースを見られた。Jaeger で、マッチ確保の競合負け（93 回）と成立（48 回）、成立時の書き込み 4 回が直列で約 220ms かかることが見えた
- AWS dev で、API Gateway → authorizer → 本体の Lambda → DynamoDB が 1 本のトレースにつながった。1.6 秒のうち、コールドスタートが authorizer で 212ms、本体で 139ms。最初の DynamoDB の Query が 626ms と突出していることが分かった（OTel だけでは、コールドスタートと API Gateway の区間は見えなかった）
- 計測のコードは送り先を知らない。送り先を変えるのは環境変数と `telemetry` パッケージだけで済む

### 悪い点・残るリスク

- **署名と Exporter の切り替えを自分で持つことになった。** OTel の Go SDK は SigV4 署名を持たないので、署名の処理を `exporter_xray.go` に書いた
- **索引付けは 1% にしている。** Span は CloudWatch Logs の `aws/spans` にすべて入るが、X-Ray のトレース一覧と詳細画面に載るのは、索引付けされた 1% だけ。それ以外は、Transaction Search の画面か Logs Insights で TraceID を指定して見る。索引付けの割合を上げると、その分の費用がかかる
- **トレースで見えるのは 1 回の呼び出しの中だけ。** 「人数が増えるとマッチ成立までの時間が伸びる」のは、大半が 2 秒ごとのポーリングの何回目で 4 人そろうかという、呼び出しと呼び出しの間の待ち時間なので、トレースでは説明できない。そこはメトリクス（`MatchWaitTime`、[ADR-0007](0007-emf-metrics.md)）で見る
- **ゲームサーバー（GameLift 上のプロセス）は計測していない。** トレースが届くのは、Lambda から GameLift API を呼ぶところまで

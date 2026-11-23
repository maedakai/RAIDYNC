# 設計判断の記録（ADR）

RAIDYNC の主要な設計判断を、1 判断 1 ファイルで記録する。各 ADR には、その判断が必要になった背景、検討した選択肢、決定、その結果（トレードオフと残るリスク）を書く。

議論の経緯や細かい Q&A は [qa-log.md](../qa-log.md) にある。ADR はそこから判断だけを取り出したもの。

| # | 判断 | ステータス |
|---|------|-----------|
| [0001](0001-optimistic-lock-matchmaking.md) | マッチ成立の排他は、チケットへの楽観ロックで行う | 採用 |
| [0002](0002-region-local-tickets.md) | MatchmakingTickets はリージョンごとに独立させ、セッションアフィニティで補う | 採用 |
| [0003](0003-udp-with-seq-gate.md) | ゲーム通信は UDP にし、信頼性は「古いパケットを捨てる」だけ自作する | 採用 |
| [0004](0004-gamelift-managed-ec2-spot.md) | ゲームサーバーは GameLift Managed EC2 で動かし、Spot をプライマリにする | 採用（On-Demand フォールバック・オートスケールは未実装） |
| [0005](0005-no-vpc.md) | 自前の VPC を作らない | 採用（計画から変更） |
| [0006](0006-no-cloudfront.md) | CloudFront を作らない | 採用（計画から変更） |
| [0007](0007-emf-metrics.md) | メトリクスは CloudWatch EMF でアプリから出す | 採用 |
| [0008](0008-opentelemetry-tracing.md) | トレースは OpenTelemetry で計測し、X-Ray の OTLP エンドポイントへ直接送る | 採用 |

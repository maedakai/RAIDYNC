"""本番規模（同時 1,000 セッション）の月額コストを試算し、docs/cost-estimate.md 用の表を出力する。

    python scripts/cost_estimate.py

前提と単価はすべてこのファイルの先頭にまとめてある。前提を変えたら、出力を docs/cost-estimate.md に貼り直す。
単価は AWS の公開価格表（https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/<service>/current/ap-northeast-1/index.json、
2026-11 取得）の ap-northeast-1。Spot と GameLift のデータ転送は価格表に無いため、下の注記どおりに置いた。
"""
import math
import sys

sys.stdout.reconfigure(encoding="utf-8")

# ---------- 前提（ワークロード） ----------
PEAK_SESSIONS = 1000          # 目標: 同時 1,000 セッション（4,000 接続）
AVG_RATIO = 0.40              # 月平均の同時セッション数 ÷ ピーク（時間帯の波を仮定）
SESSION_MIN = 20              # 1 セッションの長さ（上限いっぱいで見積もる）
PLAYERS = 4
HOURS = 720                   # 1 か月
MAU = 40_000                  # 月間アクティブユーザー（ピーク同時 4,000 人 = MAU の 10% と仮定）
BUFFER = 1.10                 # 新しいセッションを受けるための空き（10%）

AVG_SESSIONS = PEAK_SESSIONS * AVG_RATIO
SESSIONS_PER_MONTH = AVG_SESSIONS * (60 / SESSION_MIN) * HOURS

# ---------- 前提（負荷テスト 1,000 VU の実測から。1 セッション = 4 人あたり） ----------
API_PER_SESSION = 72_378 / 4_968 + 1        # match/* のリクエスト + /session/end の報告 1 回
LAMBDA_PER_API = 144_756 / 72_378           # authorizer をキャッシュしていないので 2.0
LAMBDA_MS = 31                              # match_status の平均実行時間
LAMBDA_GB = 0.25                            # 256 MB
TICKET_WRU = (379_608 + 99_744 + 99_744) / 4_968   # 本体 + GSI 2 本
TICKET_RRU = (27_489.5 + 56_024.5 + 10_000) / 4_968
GLOBAL_WRU = 1 + 10                         # Sessions の作成 1 + 終了時の更新と MatchHistory 4 件（トランザクションで 2 倍）
LOG_BYTES_PER_API = (131_807_780 - 73_481_944) / 72_378   # Lambda のログ（Span を除く）
SPAN_BYTES_PER_API = 73_481_944 / 72_378                   # 全件サンプリング時の Span
SAMPLING = 0.05                                            # 本番のサンプリング（既定ルールの 5%）

# ---------- 前提（ゲームサーバーの送信量） ----------
STATE_BYTES = 840 + 28        # SESSION_STATE（4 人分の JSON）+ UDP/IP ヘッダー
TICK_HZ = 20
GB_PER_SESSION = STATE_BYTES * TICK_HZ * PLAYERS * SESSION_MIN * 60 / 1e9

# ---------- 単価（USD、ap-northeast-1） ----------
GL_OD = {"c5.large": 0.137, "c6g.large": 0.111}   # GameLift Linux On-Demand / 時間
SPOT_RATIO = 0.30             # Spot は変動価格で価格表に無い。AWS の「50〜85% 安い」と公式の計算例（約 70% 引き）から 30% と置く
DTO_PER_GB = 0.09             # GameLift のデータ転送（第 5 世代のみ課金）。AWS の料金ページの最初の段の値
FREE_BANDWIDTH = {"c5.large": False, "c6g.large": True}   # 第 6 世代以降は転送無料
API_PER_M = 4.25
LAMBDA_REQ_PER_M = 0.20
LAMBDA_GBS = 0.0000133334     # arm64
LAMBDA_FREE_REQ, LAMBDA_FREE_GBS = 1_000_000, 400_000
DDB_WRU_PER_M, DDB_RRU_PER_M, DDB_RWRU_PER_M = 0.715, 0.1425, 0.715
COGNITO_ESSENTIALS, COGNITO_FREE = 0.015, 10_000
LOG_PER_GB = 0.76
METRIC, ALARM, DASHBOARD = 0.30, 0.10, 3.00
R53_ZONE, R53_LBR_PER_M = 0.50, 0.60


def money(v):
    return f"${v:,.0f}" if v >= 10 else f"${v:,.2f}"


def gamelift(instance, per_instance, spot, autoscale):
    sessions = AVG_SESSIONS if autoscale else PEAK_SESSIONS
    instances = math.ceil(sessions / per_instance * BUFFER)
    price = GL_OD[instance] * (SPOT_RATIO if spot else 1)
    compute = instances * price * HOURS
    dto = 0 if FREE_BANDWIDTH[instance] else SESSIONS_PER_MONTH * GB_PER_SESSION * DTO_PER_GB
    return instances, compute, dto


def backend():
    api_req = SESSIONS_PER_MONTH * API_PER_SESSION
    lam_req = api_req * LAMBDA_PER_API
    lam_gbs = lam_req * LAMBDA_MS / 1000 * LAMBDA_GB
    rows = [
        ("API Gateway（REST）", f"{api_req / 1e6:,.1f}M リクエスト", api_req / 1e6 * API_PER_M),
        ("Lambda", f"{lam_req / 1e6:,.1f}M 回・{lam_gbs / 1e3:,.0f}K GB 秒",
         max(lam_req - LAMBDA_FREE_REQ, 0) / 1e6 * LAMBDA_REQ_PER_M + max(lam_gbs - LAMBDA_FREE_GBS, 0) * LAMBDA_GBS),
        ("DynamoDB MatchmakingTickets", f"{SESSIONS_PER_MONTH * TICKET_WRU / 1e6:,.0f}M WRU・{SESSIONS_PER_MONTH * TICKET_RRU / 1e6:,.0f}M RRU",
         SESSIONS_PER_MONTH * (TICKET_WRU * DDB_WRU_PER_M + TICKET_RRU * DDB_RRU_PER_M) / 1e6),
        ("DynamoDB Global Tables", f"{SESSIONS_PER_MONTH * GLOBAL_WRU / 1e6:,.1f}M WRU + 同量のレプリケーション",
         SESSIONS_PER_MONTH * GLOBAL_WRU * (DDB_WRU_PER_M + DDB_RWRU_PER_M) / 1e6),
        ("Cognito（Essentials）", f"{MAU:,} MAU", max(MAU - COGNITO_FREE, 0) * COGNITO_ESSENTIALS),
        ("CloudWatch Logs", f"{api_req * (LOG_BYTES_PER_API + SPAN_BYTES_PER_API * SAMPLING) / 1e9:,.1f} GB",
         api_req * (LOG_BYTES_PER_API + SPAN_BYTES_PER_API * SAMPLING) / 1e9 * LOG_PER_GB),
        ("CloudWatch メトリクス・アラーム・ダッシュボード", "30 メトリクス・12 アラーム・1 枚", 30 * METRIC + 12 * ALARM + DASHBOARD),
        ("Route 53", f"ホストゾーン 1・レイテンシークエリ {SESSIONS_PER_MONTH * PLAYERS / 1e6:,.1f}M",
         R53_ZONE + SESSIONS_PER_MONTH * PLAYERS / 1e6 * R53_LBR_PER_M),
    ]
    return rows


def main():
    print(f"<!-- scripts/cost_estimate.py の出力 -->\n")
    print("### 前提から決まる量\n")
    print("| 項目 | 値 |\n|------|----|")
    print(f"| 月平均の同時セッション | {AVG_SESSIONS:,.0f}（ピーク {PEAK_SESSIONS:,} × {AVG_RATIO:.0%}） |")
    print(f"| 月間セッション数 | {SESSIONS_PER_MONTH:,.0f} |")
    print(f"| 1 セッションあたりの API リクエスト | {API_PER_SESSION:.1f} |")
    print(f"| 1 セッションあたりのゲームサーバーの送信量 | {GB_PER_SESSION * 1000:,.0f} MB |")
    print(f"| 月間のゲームサーバーの送信量 | {SESSIONS_PER_MONTH * GB_PER_SESSION / 1000:,.0f} TB |\n")

    print("### ゲームサーバー（GameLift）\n")
    print("| # | 構成 | 平均台数 | インスタンス | データ転送 | 月額 | 1 つ前からの削減 |")
    print("|---|------|-------:|-----------:|---------:|-----:|------:|")
    steps = [
        ("今の設計を On-Demand で動かす（c5.large・1 台 1 セッション・台数固定）", "c5.large", 1, False, False),
        ("Spot にする（今の Terraform の設定）", "c5.large", 1, True, False),
        ("オートスケールで需要に合わせる", "c5.large", 1, True, True),
        ("1 台に 8 プロセス載せる", "c5.large", 8, True, True),
        ("Graviton（c6g.large）に変える（転送無料）", "c6g.large", 8, True, True),
    ]
    prev = None
    for i, (name, inst, per, spot, auto) in enumerate(steps, 1):
        n, c, d = gamelift(inst, per, spot, auto)
        total = c + d
        cut = "—" if prev is None else f"−{(1 - total / prev):.0%}"
        print(f"| {i} | {name} | {n:,} | {money(c)} | {money(d)} | **{money(total)}** | {cut} |")
        prev = total
    print()

    print("### 1 台に載せるプロセス数と月額（構成 5 の c6g.large・Spot・オートスケール）\n")
    print("| 1 台のプロセス数 | 平均台数 | 月額 |\n|---:|---:|---:|")
    for per in (1, 4, 8, 16, 32):
        n, c, d = gamelift("c6g.large", per, True, True)
        print(f"| {per} | {n:,} | {money(c + d)} |")
    print()

    print("### マッチメイキングと周辺\n")
    print("| サービス | 量（月） | 月額 |\n|---------|---------|-----:|")
    total = 0
    for name, qty, cost in backend():
        print(f"| {name} | {qty} | {money(cost)} |")
        total += cost
    print(f"| **合計** | | **{money(total)}** |")


if __name__ == "__main__":
    main()

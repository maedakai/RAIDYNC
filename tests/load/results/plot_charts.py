"""cloudwatch-2026-11.json から、負荷テスト結果レポート用の SVG グラフを作る。

    python tests/load/results/plot_charts.py tests/load/results/cloudwatch-2026-11.json docs/images/load-test

GitHub の README / Markdown に <img> で埋め込む前提の静的 SVG。ライト／ダークは OS の設定に従う。
"""
import json
import os
import sys
from datetime import datetime, timedelta, timezone
from xml.sax.saxutils import escape

SRC, OUT = sys.argv[1], sys.argv[2]
os.makedirs(OUT, exist_ok=True)
data = json.load(open(SRC, encoding="utf-8"))
runs = {r["vus"]: r for r in data["runs"]}

JST = timezone(timedelta(hours=9))
FONT = "system-ui, -apple-system, 'Segoe UI', 'Hiragino Sans', 'Yu Gothic UI', Meiryo, sans-serif"

STYLE = f"""
<style>
  svg {{ font-family: {FONT}; }}
  .surface {{ fill: #fcfcfb; }}
  .t1 {{ fill: #0b0b0b; }}
  .t2 {{ fill: #52514e; }}
  .muted {{ fill: #898781; }}
  .grid {{ stroke: #e1e0d9; stroke-width: 1; }}
  .axis {{ stroke: #c3c2b7; stroke-width: 1; }}
  .ref {{ stroke: #52514e; stroke-width: 1.5; }}
  .s1 {{ fill: #2a78d6; }} .s1l {{ stroke: #2a78d6; }}
  .s2 {{ fill: #eb6834; }} .s2l {{ stroke: #eb6834; }}
  .ring {{ stroke: #fcfcfb; stroke-width: 2; }}
  @media (prefers-color-scheme: dark) {{
    .surface {{ fill: #1a1a19; }}
    .t1 {{ fill: #ffffff; }}
    .t2 {{ fill: #c3c2b7; }}
    .grid {{ stroke: #2c2c2a; }}
    .axis {{ stroke: #383835; }}
    .ref {{ stroke: #c3c2b7; }}
    .s1 {{ fill: #3987e5; }} .s1l {{ stroke: #3987e5; }}
    .s2 {{ fill: #d95926; }} .s2l {{ stroke: #d95926; }}
    .ring {{ stroke: #1a1a19; }}
  }}
</style>"""


def svg(w, h, body, title, desc):
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="{w}" height="{h}" viewBox="0 0 {w} {h}" '
            f'role="img" aria-labelledby="t d">\n<title id="t">{escape(title)}</title>\n'
            f'<desc id="d">{escape(desc)}</desc>{STYLE}\n'
            f'<rect class="surface" width="{w}" height="{h}" rx="8"/>\n{body}\n</svg>\n')


def text(x, y, s, cls="t2", size=12, anchor="start", weight=400):
    return (f'<text x="{x:.1f}" y="{y:.1f}" class="{cls}" font-size="{size}" text-anchor="{anchor}" '
            f'font-weight="{weight}">{escape(s)}</text>')


def nice_axis(v):
    """v を収める、きりのよい目盛り (上端, 本数) を返す。刻みは 1/2/2.5/5 × 10^k、本数は 3〜5 本から上端が最小のもの。"""
    best = None
    for n in (3, 4, 5):
        raw = v / n
        for mag in range(-1, 7):
            hit = next((st * 10 ** mag for st in (1, 2, 2.5, 5) if st * 10 ** mag >= raw
                        and float(st * 10 ** mag).is_integer()), None)
            if hit:
                break
        top = hit * n
        if best is None or top < best[0]:
            best = (top, n)
    return best


def fmt(v):
    return f"{v:,.0f}" if v >= 10 else f"{v:g}"


def column_top(x, y_top, w, y_base, cls):
    """4px の角丸はデータ側の端だけ。根元は四角。"""
    r = min(4, (y_base - y_top) / 2, w / 2)
    return (f'<path class="{cls}" d="M{x:.1f},{y_base:.1f} V{y_top + r:.1f} Q{x:.1f},{y_top:.1f} {x + r:.1f},{y_top:.1f} '
            f'H{x + w - r:.1f} Q{x + w:.1f},{y_top:.1f} {x + w:.1f},{y_top + r:.1f} V{y_base:.1f} Z"/>')


def bar_right(x_base, y, x_end, h, cls):
    r = min(4, (x_end - x_base) / 2, h / 2)
    return (f'<path class="{cls}" d="M{x_base:.1f},{y:.1f} H{x_end - r:.1f} Q{x_end:.1f},{y:.1f} {x_end:.1f},{y + r:.1f} '
            f'V{y + h - r:.1f} Q{x_end:.1f},{y + h:.1f} {x_end - r:.1f},{y + h:.1f} H{x_base:.1f} Z"/>')


# ---------- 1. 1,000 VU の 1 分ごとの推移（スモールマルチプル） ----------
def series(run, key, scale=1.0):
    return {p["t"]: p["v"] * scale for p in run["series"][key]}


r1k = runs[1000]
minutes = sorted(series(r1k, "match_formed"))  # マッチが成立していた 5 分間
labels = [datetime.fromisoformat(t.replace("Z", "+00:00")).astimezone(JST).strftime("%H:%M") for t in minutes]

panels = [
    ("API リクエスト数（件/分）", [("リクエスト数", "s1", series(r1k, "api_count"))]),
    ("Lambda 同時実行数（1 分間の最大）", [("同時実行数", "s1", series(r1k, "lambda_concurrency_max"))]),
    ("API の応答時間 p95（ms）", [("p95", "s1", series(r1k, "api_latency_p95"))]),
    ("マッチ成立までの待ち時間（秒）", [("p50", "s1", series(r1k, "match_wait_p50", 0.001)),
                                       ("p95", "s2", series(r1k, "match_wait_p95", 0.001))]),
]

PW, PH = 400, 210
W1 = 2 * PW + 60
H1 = 2 * PH + 120
body = [text(24, 34, "1,000 VU 実行中の 1 分ごとの推移（2026-11-20 JST）", "t1", 16, weight=600),
        text(24, 54, "負荷が最も高い 00:10 に待ち時間が最も伸びる一方、API の応答時間は横ばい", "t2", 12)]
for i, (ptitle, lines) in enumerate(panels):
    ox = 24 + (i % 2) * (PW + 12)
    oy = 76 + (i // 2) * (PH + 18)
    left, right, top, bottom = ox + 52, ox + PW - 40, oy + 34, oy + PH - 28
    body.append(text(ox, oy + 14, ptitle, "t1", 13, weight=600))
    vmax, nt = nice_axis(max(v for _, _, s in lines for v in s.values()) * 1.05)
    for k in range(nt + 1):
        y = bottom - (bottom - top) * k / nt
        body.append(f'<line class="grid" x1="{left}" x2="{right}" y1="{y:.1f}" y2="{y:.1f}"/>')
        body.append(text(left - 6, y + 4, fmt(vmax * k / nt), "muted", 11, "end"))
    body.append(f'<line class="axis" x1="{left}" x2="{right}" y1="{bottom}" y2="{bottom}"/>')
    xs = [left + 12 + (right - left - 24) * j / (len(minutes) - 1) for j in range(len(minutes))]
    for j, lab in enumerate(labels):
        body.append(text(xs[j], bottom + 16, lab, "muted", 11, "middle"))
    if len(lines) > 1:  # 2 系列のときは凡例を置く
        lx = right - 110
        for n, (name, cls, _) in enumerate(lines):
            body.append(f'<line class="{cls}l" x1="{lx + n * 56}" x2="{lx + n * 56 + 14}" y1="{oy + 10}" y2="{oy + 10}" '
                        f'stroke-width="2" stroke-linecap="round"/>')
            body.append(text(lx + n * 56 + 18, oy + 14, name, "t2", 11))
    for name, cls, s in lines:
        pts = [(xs[j], bottom - (bottom - top) * s[t] / vmax) for j, t in enumerate(minutes)]
        d = " ".join(f"{x:.1f},{y:.1f}" for x, y in pts)
        body.append(f'<polyline class="{cls}l" points="{d}" fill="none" stroke-width="2" '
                    f'stroke-linejoin="round" stroke-linecap="round"/>')
        peak = max(range(len(pts)), key=lambda j: s[minutes[j]])
        for j, (x, y) in enumerate(pts):
            v = s[minutes[j]]
            body.append(f'<circle class="{cls} ring" cx="{x:.1f}" cy="{y:.1f}" r="4"><title>{labels[j]} {name}: {v:,.1f}</title></circle>')
        # ピークだけ直接ラベルを付ける
        px, py = pts[peak]
        body.append(text(px, py - 9, f"{s[minutes[peak]]:,.0f}" if s[minutes[peak]] >= 10 else f"{s[minutes[peak]]:.1f}",
                         "t1", 11, "middle", 600))
body.append(text(24, H1 - 16, "出典: CloudWatch（1 分粒度）。待ち時間は 4 人で成立したセッションの MatchWaitTime。", "muted", 11))
open(os.path.join(OUT, "timeline-1000vu.svg"), "w", encoding="utf-8").write(
    svg(W1, H1, "\n".join(body), "1,000 VU 実行中の 1 分ごとの推移",
        "API リクエスト数、Lambda 同時実行数、API 応答時間 p95、マッチ待ち時間 p50/p95 の 5 分間の推移"))

# ---------- 2. 規模ごとのマッチ待ち時間 ----------
W2, H2 = 640, 340
left, right, top, bottom = 70, W2 - 30, 90, H2 - 56
groups = [40, 400, 1000]
vals = {vu: (runs[vu]["summary"]["match_wait_p50"] / 1000, runs[vu]["summary"]["match_wait_p95"] / 1000) for vu in groups}
vmax, nt = nice_axis(max(max(v) for v in vals.values()) * 1.1)
body = [text(24, 34, "規模ごとのマッチ成立までの待ち時間", "t1", 16, weight=600),
        text(24, 54, "API の応答は速いままでも、人数が増えるほど 4 人そろうまでの待ちが伸びる", "t2", 12)]
lx = right - 120
for n, (name, cls) in enumerate((("p50", "s1"), ("p95", "s2"))):
    body.append(f'<rect class="{cls}" x="{lx + n * 60}" y="64" width="12" height="12" rx="2"/>')
    body.append(text(lx + n * 60 + 18, 75, name, "t2", 12))
for k in range(nt + 1):
    y = bottom - (bottom - top) * k / nt
    body.append(f'<line class="grid" x1="{left}" x2="{right}" y1="{y:.1f}" y2="{y:.1f}"/>')
    body.append(text(left - 8, y + 4, f"{vmax * k / nt:g} 秒", "muted", 11, "end"))
band = (right - left) / len(groups)
BW, GAP = 24, 2
for gi, vu in enumerate(groups):
    cx = left + band * gi + band / 2
    for n, cls in enumerate(("s1", "s2")):
        v = vals[vu][n]
        x = cx - BW - GAP / 2 if n == 0 else cx + GAP / 2
        y = bottom - (bottom - top) * v / vmax
        body.append(column_top(x, y, BW, bottom, cls))
        body.append(text(x + BW / 2, y - 6, f"{v:.1f}", "t1", 11, "middle", 600))
    body.append(text(cx, bottom + 18, f"{vu:,} VU", "t2", 12, "middle"))
body.append(f'<line class="axis" x1="{left}" x2="{right}" y1="{bottom}" y2="{bottom}"/>')
body.append(text(24, H2 - 14, "出典: CloudWatch RAIDYNC/Matchmaking MatchWaitTime（PartySize=4）を各回の時間帯全体で集計", "muted", 11))
open(os.path.join(OUT, "match-wait-by-scale.svg"), "w", encoding="utf-8").write(
    svg(W2, H2, "\n".join(body), "規模ごとのマッチ成立までの待ち時間",
        "40/400/1,000 VU それぞれの待ち時間 p50 と p95（秒）"))

# ---------- 3. 上限に対する使用率（1,000 VU 実測と 4,000 VU 推定） ----------
s1k = r1k["series"]
lam = max(p["v"] for p in s1k["lambda_concurrency_max"])
rps = max(p["v"] for p in s1k["api_count"]) / 60
gsi = max(p["v"] for p in s1k["gsi_wcu"]) / 60
rows = [  # (名前, 1,000 VU 実測, 4,000 VU 推定, 上限, 単位)
    ("Lambda 同時実行", lam, lam * 4, 1000, ""),
    ("API Gateway（RPS）", rps, rps * 4, 10000, ""),
    ("GSI 書き込み（WCU/秒）", gsi, gsi * 4, 1000, ""),
]
W3, H3 = 720, 350
left, right, top = 190, W3 - 90, 110
xmax = 250  # %
body = [text(24, 34, "上限に対する使用率（1,000 VU 実測と 4,000 VU 推定）", "t1", 16, weight=600),
        text(24, 54, "4,000 VU では GSI の書き込みだけが 1 パーティションキーの上限を超える見込み", "t2", 12)]
lx = 24
for n, (name, cls) in enumerate((("1,000 VU 実測", "s1"), ("4,000 VU 推定（×4）", "s2"))):
    body.append(f'<rect class="{cls}" x="{lx + n * 105}" y="64" width="12" height="12" rx="2"/>')
    body.append(text(lx + n * 105 + 18, 75, name, "t2", 12))
rowh = 62
bottom = top + rowh * len(rows)
for k in range(0, xmax + 1, 50):
    x = left + (right - left) * k / xmax
    body.append(f'<line class="grid" x1="{x:.1f}" x2="{x:.1f}" y1="{top - 6}" y2="{bottom}"/>')
    body.append(text(x, bottom + 16, f"{k}%", "muted", 11, "middle"))
for i, (name, meas, est, limit, _) in enumerate(rows):
    y0 = top + rowh * i + 10
    body.append(text(left - 12, y0 + 22, name, "t1", 12, "end", 600))
    body.append(text(left - 12, y0 + 38, f"上限 {limit:,}", "muted", 11, "end"))
    for n, (v, cls) in enumerate(((meas, "s1"), (est, "s2"))):
        pct = v / limit * 100
        y = y0 + n * 22
        x_end = left + (right - left) * min(pct, xmax) / xmax
        body.append(bar_right(left, y, max(x_end, left + 2), 18, cls))
        body.append(text(x_end + 6, y + 13, f"{pct:.0f}%（{v:,.0f}）", "t1", 11, "start", 600))
x100 = left + (right - left) * 100 / xmax
body.append(f'<line class="ref" x1="{x100:.1f}" x2="{x100:.1f}" y1="{top - 10}" y2="{bottom + 2}"/>')
body.append(text(x100 + 4, top - 12, "上限（100%）", "t2", 11))
body.append(f'<line class="axis" x1="{left}" x2="{left}" y1="{top - 6}" y2="{bottom}"/>')
body.append(text(24, H3 - 14, "1,000 VU は最も負荷の高い 1 分間の値。4,000 VU は単純に 4 倍した推定（実測していない）。", "muted", 11))
open(os.path.join(OUT, "capacity-headroom.svg"), "w", encoding="utf-8").write(
    svg(W3, H3, "\n".join(body), "上限に対する使用率",
        "Lambda 同時実行、API Gateway RPS、GSI 書き込みの、1,000 VU 実測と 4,000 VU 推定の上限比"))
print("ok")

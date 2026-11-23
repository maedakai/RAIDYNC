"""負荷テスト（6-6）の各回について、CloudWatch の 1 分粒度のメトリクスを取得して JSON に保存する。

CloudWatch の 1 分粒度のデータは 15 日で 5 分粒度にまとめられるため、テスト後なるべく早く取得しておく。
実行には AWS CLI と、cloudwatch:GetMetricData の権限が要る。

    python tests/load/results/fetch_cloudwatch.py tests/load/results/cloudwatch-2026-11.json
"""
import json
import subprocess
import sys
import tempfile

REGION = "ap-northeast-1"

# 各回の負荷の時間帯（UTC）。API Gateway の 1 分ごとのリクエスト数から特定した。
RUNS = [
    {"vus": 40, "label": "40 VU × 20 回", "start": "2026-11-18T14:28:00Z", "end": "2026-11-18T14:34:00Z"},
    {"vus": 400, "label": "400 VU × 20 回（PERIOD 0.05）", "start": "2026-11-18T15:35:00Z", "end": "2026-11-18T15:41:00Z"},
    {"vus": 1000, "label": "1,000 VU × 20 回（PERIOD 0.1）", "start": "2026-11-19T15:05:00Z", "end": "2026-11-19T15:14:00Z"},
]


def metric(id_, ns, name, dims, stat, period=60):
    return {
        "Id": id_,
        "MetricStat": {
            "Metric": {"Namespace": ns, "MetricName": name,
                       "Dimensions": [{"Name": k, "Value": v} for k, v in dims.items()]},
            "Period": period,
            "Stat": stat,
        },
    }


API = {"ApiName": "raidync-dev-api"}
TBL = {"TableName": "MatchmakingTickets"}
GSI = {"TableName": "MatchmakingTickets", "GlobalSecondaryIndexName": "questId-status-index"}
P4 = {"PartySize": "4"}

QUERIES = [
    metric("api_count", "AWS/ApiGateway", "Count", API, "Sum"),
    metric("api_latency_p95", "AWS/ApiGateway", "Latency", API, "p95"),
    metric("api_5xx", "AWS/ApiGateway", "5XXError", API, "Sum"),
    metric("api_4xx", "AWS/ApiGateway", "4XXError", API, "Sum"),
    metric("lambda_concurrency_max", "AWS/Lambda", "ConcurrentExecutions", {}, "Maximum"),
    metric("lambda_throttles", "AWS/Lambda", "Throttles", {}, "Sum"),
    metric("lambda_errors", "AWS/Lambda", "Errors", {}, "Sum"),
    metric("gsi_wcu", "AWS/DynamoDB", "ConsumedWriteCapacityUnits", GSI, "Sum"),
    metric("table_wcu", "AWS/DynamoDB", "ConsumedWriteCapacityUnits", TBL, "Sum"),
    metric("table_write_throttles", "AWS/DynamoDB", "WriteThrottleEvents", TBL, "Sum"),
    metric("gsi_write_throttles", "AWS/DynamoDB", "WriteThrottleEvents", GSI, "Sum"),
    metric("match_formed", "RAIDYNC/Matchmaking", "MatchFormed", P4, "Sum"),
    metric("match_wait_p50", "RAIDYNC/Matchmaking", "MatchWaitTime", P4, "p50"),
    metric("match_wait_p95", "RAIDYNC/Matchmaking", "MatchWaitTime", P4, "p95"),
]


def summary_queries(period):
    """時間帯全体を 1 つの期間にした集計。1 分ごとのパーセンタイルからは全体のパーセンタイルを出せないため。"""
    q = []
    for stat in ("p50", "p95", "p99", "Maximum"):
        sid = stat.lower()
        q.append(metric(f"api_latency_{sid}", "AWS/ApiGateway", "Latency", API, stat, period))
        q.append(metric(f"match_wait_{sid}", "RAIDYNC/Matchmaking", "MatchWaitTime", P4, stat, period))
    q.append(metric("match_formed", "RAIDYNC/Matchmaking", "MatchFormed", P4, "Sum", period))
    q.append(metric("api_count", "AWS/ApiGateway", "Count", API, "Sum", period))
    return q


def seconds(run):
    from datetime import datetime
    f = "%Y-%m-%dT%H:%M:%SZ"
    return int((datetime.strptime(run["end"], f) - datetime.strptime(run["start"], f)).total_seconds())


def fetch(run, queries=QUERIES):
    with tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8") as f:
        json.dump(queries, f)
        path = f.name
    out = subprocess.run(
        ["aws", "cloudwatch", "get-metric-data", "--region", REGION,
         "--start-time", run["start"], "--end-time", run["end"],
         "--metric-data-queries", f"file://{path}", "--output", "json"],
        check=True, capture_output=True, text=True).stdout
    series = {}
    for r in json.loads(out)["MetricDataResults"]:
        pts = sorted(zip(r["Timestamps"], r["Values"]))
        series[r["Id"]] = [{"t": t, "v": v} for t, v in pts]
    return series


def main():
    result = {"region": REGION, "period_seconds": 60, "runs": []}
    for run in RUNS:
        summary = {k: (v[0]["v"] if v else None) for k, v in fetch(run, summary_queries(seconds(run))).items()}
        result["runs"].append({**run, "summary": summary, "series": fetch(run)})
    with open(sys.argv[1], "w", encoding="utf-8") as f:
        json.dump(result, f, ensure_ascii=False, indent=1)


if __name__ == "__main__":
    main()

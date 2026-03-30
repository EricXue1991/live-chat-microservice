#!/usr/bin/env python3
"""
Experiment 4 charts from two Locust locust_stats.csv files (polling vs WebSocket).

  pip install matplotlib
  python scripts/plot_experiment4.py \
    --polling-csv scripts/results/exp4_xxx_polling/locust_stats.csv \
    --ws-csv      scripts/results/exp4_xxx_ws/locust_stats.csv

PNG output: report/figures/exp4/ (created if missing)
"""
from __future__ import annotations

import argparse
import csv
from pathlib import Path


def find_latency_row(csv_path: Path, request_type: str) -> dict[str, str]:
    """Extract the e2e_delivery row for the given request_type (POLL_LATENCY or WS_LATENCY)."""
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Type") == request_type and row.get("Name") == "e2e_delivery":
                return row
    raise SystemExit(
        f"No '{request_type}' / 'e2e_delivery' row found in {csv_path}.\n"
        "Make sure you ran PollingUser (for polling CSV) and WebSocketUser (for WS CSV)."
    )


def fnum(row: dict[str, str], key: str) -> float:
    return float(row[key].strip())


def main() -> None:
    ap = argparse.ArgumentParser(description="Plot Experiment 4 latency charts from Locust CSVs.")
    ap.add_argument("--polling-csv", type=Path, required=True, dest="polling_csv",
                    help="locust_stats.csv from PollingUser run")
    ap.add_argument("--ws-csv", type=Path, required=True, dest="ws_csv",
                    help="locust_stats.csv from WebSocketUser run")
    ap.add_argument(
        "--out-dir", type=Path,
        default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp4",
        help="directory for PNG output files",
    )
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError as e:
        raise SystemExit("Need matplotlib: pip install matplotlib") from e

    row_poll = find_latency_row(args.polling_csv, "POLL_LATENCY")
    row_ws   = find_latency_row(args.ws_csv,      "WS_LATENCY")

    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)

    # --- Chart 1: Latency percentiles (p50, p95, p99) side-by-side ---
    metrics = [("p50", "50%"), ("p95", "95%"), ("p99", "99%")]
    poll_vals = [fnum(row_poll, key) for _, key in metrics]
    ws_vals   = [fnum(row_ws,   key) for _, key in metrics]

    fig, ax = plt.subplots(figsize=(7, 4))
    w = 0.35
    x = list(range(len(metrics)))
    bars_poll = ax.bar([i - w / 2 for i in x], poll_vals, width=w,
                       label="HTTP Polling", color="#c44e52")
    bars_ws   = ax.bar([i + w / 2 for i in x], ws_vals,   width=w,
                       label="WebSocket",    color="#4c72b0")

    # Label each bar with its value
    for b, v in zip(bars_poll, poll_vals):
        ax.text(b.get_x() + b.get_width() / 2, b.get_height() + 5,
                f"{v:.0f}ms", ha="center", va="bottom", fontsize=9)
    for b, v in zip(bars_ws, ws_vals):
        ax.text(b.get_x() + b.get_width() / 2, b.get_height() + 5,
                f"{v:.0f}ms", ha="center", va="bottom", fontsize=9)

    ax.set_xticks(x)
    ax.set_xticklabels([m[0] for m in metrics])
    ax.set_ylabel("End-to-end delivery latency (ms)")
    ax.set_title("Experiment 4: WebSocket vs HTTP Polling — latency percentiles")
    ax.legend()
    fig.tight_layout()
    p1 = out / "exp4_latency_percentiles.png"
    fig.savefig(p1, dpi=150)
    plt.close(fig)

    # --- Chart 2: Average latency comparison ---
    avg_poll = fnum(row_poll, "Average Response Time")
    avg_ws   = fnum(row_ws,   "Average Response Time")
    labels = ["HTTP Polling", "WebSocket"]
    values = [avg_poll, avg_ws]

    fig, ax = plt.subplots(figsize=(5, 4))
    bars = ax.bar(labels, values, color=["#c44e52", "#4c72b0"])
    for b, v in zip(bars, values):
        ax.text(b.get_x() + b.get_width() / 2, b.get_height() + 5,
                f"{v:.0f}ms", ha="center", va="bottom", fontsize=11)
    ax.set_ylabel("Average end-to-end delivery latency (ms)")
    ax.set_title("Experiment 4: WebSocket vs HTTP Polling — average latency")
    fig.tight_layout()
    p2 = out / "exp4_avg_latency.png"
    fig.savefig(p2, dpi=150)
    plt.close(fig)

    # --- Print summary ---
    print("=== Experiment 4 Results ===")
    print(f"{'Metric':<12} {'HTTP Polling':>14} {'WebSocket':>12} {'Speedup':>10}")
    print("-" * 52)
    for label, pv, wv in zip(["p50", "p95", "p99"], poll_vals, ws_vals):
        speedup = pv / wv if wv > 0 else float("inf")
        print(f"{label:<12} {pv:>12.0f}ms {wv:>10.0f}ms {speedup:>9.1f}x")
    speedup_avg = avg_poll / avg_ws if avg_ws > 0 else float("inf")
    print(f"{'average':<12} {avg_poll:>12.0f}ms {avg_ws:>10.0f}ms {speedup_avg:>9.1f}x")
    print("")
    print(f"Wrote {p1}")
    print(f"Wrote {p2}")


if __name__ == "__main__":
    main()

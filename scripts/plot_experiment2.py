#!/usr/bin/env python3
"""
Experiment 2 — Hot-Room vs Multi-Room chart generator.

Usage:
    pip install matplotlib
    python scripts/plot_experiment2.py \
        --hot-csv   scripts/results/exp2_xxx/hot/locust_stats.csv \
        --multi-csv scripts/results/exp2_xxx/multi/locust_stats.csv

Optional time-series (if --hot-history / --multi-history provided):
    python scripts/plot_experiment2.py \
        --hot-csv     scripts/results/exp2_xxx/hot/locust_stats.csv \
        --multi-csv   scripts/results/exp2_xxx/multi/locust_stats.csv \
        --hot-history scripts/results/exp2_xxx/hot/locust_stats_history.csv \
        --multi-history scripts/results/exp2_xxx/multi/locust_stats_history.csv

Output: report/figures/exp2/  (4 PNG files)
"""

from __future__ import annotations
import argparse
import csv
from pathlib import Path


def load_endpoint_rows(csv_path: Path) -> dict[str, dict]:
    """Load Locust stats CSV and return rows keyed by Name."""
    rows = {}
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            name = row.get("Name", "").strip()
            if name and name != "Aggregated":
                rows[name] = row
    return rows


def load_aggregated(csv_path: Path) -> dict:
    """Load the Aggregated row from Locust stats CSV."""
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Name", "").strip() == "Aggregated":
                return row
    return {}


def load_history(csv_path: Path) -> list[dict]:
    """Load Locust stats_history CSV."""
    with csv_path.open(newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def fnum(row: dict, key: str, default: float = 0.0) -> float:
    try:
        return float(row.get(key, default))
    except (ValueError, TypeError):
        return default


def main():
    ap = argparse.ArgumentParser(description="Plot Experiment 2 charts.")
    ap.add_argument("--hot-csv", type=Path, required=True, dest="hot_csv")
    ap.add_argument("--multi-csv", type=Path, required=True, dest="multi_csv")
    ap.add_argument("--hot-history", type=Path, default=None, dest="hot_history")
    ap.add_argument("--multi-history", type=Path, default=None, dest="multi_history")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp2")
    args = ap.parse_args()

    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("Install matplotlib:  pip install matplotlib")

    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)

    hot_rows = load_endpoint_rows(args.hot_csv)
    multi_rows = load_endpoint_rows(args.multi_csv)
    hot_agg = load_aggregated(args.hot_csv)
    multi_agg = load_aggregated(args.multi_csv)

    # Endpoints to compare
    endpoints = [
        "POST /api/messages",
        "POST /api/reactions",
        "GET /api/messages",
        "GET /api/reactions",
    ]

    colors_hot = "#e74c3c"
    colors_multi = "#3498db"

    # =========================================================================
    # Chart 1: Throughput comparison (req/s per endpoint)
    # =========================================================================
    fig, ax = plt.subplots(figsize=(10, 5))
    x = range(len(endpoints))
    w = 0.35

    hot_rps = [fnum(hot_rows.get(ep, {}), "Requests/s") for ep in endpoints]
    multi_rps = [fnum(multi_rows.get(ep, {}), "Requests/s") for ep in endpoints]

    bars1 = ax.bar([i - w/2 for i in x], hot_rps, w, label="Hot Room", color=colors_hot, alpha=0.85)
    bars2 = ax.bar([i + w/2 for i in x], multi_rps, w, label="Multi Room", color=colors_multi, alpha=0.85)

    ax.set_xticks(list(x))
    ax.set_xticklabels([ep.split(" ")[1] for ep in endpoints], rotation=15, ha="right", fontsize=9)
    ax.set_ylabel("Requests/s")
    ax.set_title("Experiment 2: Throughput — Hot Room vs Multi Room")
    ax.legend()

    for bars in [bars1, bars2]:
        for b in bars:
            h = b.get_height()
            if h > 0:
                ax.text(b.get_x() + b.get_width()/2, h, f"{h:.1f}",
                        ha="center", va="bottom", fontsize=8)

    fig.tight_layout()
    p = out / "exp2_throughput.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 2: Latency percentiles (p50, p95, p99) — grouped by endpoint
    # =========================================================================
    percentiles = [("50%", "p50"), ("95%", "p95"), ("99%", "p99")]

    fig, axes = plt.subplots(1, len(endpoints), figsize=(16, 5), sharey=False)
    if len(endpoints) == 1:
        axes = [axes]

    for idx, ep in enumerate(endpoints):
        ax = axes[idx]
        hot_vals = [fnum(hot_rows.get(ep, {}), pkey) for pkey, _ in percentiles]
        multi_vals = [fnum(multi_rows.get(ep, {}), pkey) for pkey, _ in percentiles]

        px = range(len(percentiles))
        ax.bar([i - w/2 for i in px], hot_vals, w, label="Hot", color=colors_hot, alpha=0.85)
        ax.bar([i + w/2 for i in px], multi_vals, w, label="Multi", color=colors_multi, alpha=0.85)

        ax.set_xticks(list(px))
        ax.set_xticklabels([plabel for _, plabel in percentiles])
        ax.set_ylabel("Latency (ms)")
        ax.set_title(ep.split(" ")[1], fontsize=10)
        if idx == 0:
            ax.legend(fontsize=8)

    fig.suptitle("Experiment 2: Latency Percentiles — Hot Room vs Multi Room", fontsize=12)
    fig.tight_layout()
    p = out / "exp2_latency.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 3: Error rate comparison
    # =========================================================================
    fig, ax = plt.subplots(figsize=(8, 5))

    hot_errors = [fnum(hot_rows.get(ep, {}), "Failure Count") for ep in endpoints]
    multi_errors = [fnum(multi_rows.get(ep, {}), "Failure Count") for ep in endpoints]
    hot_totals = [fnum(hot_rows.get(ep, {}), "Request Count") for ep in endpoints]
    multi_totals = [fnum(multi_rows.get(ep, {}), "Request Count") for ep in endpoints]

    hot_err_pct = [100.0 * e / t if t > 0 else 0 for e, t in zip(hot_errors, hot_totals)]
    multi_err_pct = [100.0 * e / t if t > 0 else 0 for e, t in zip(multi_errors, multi_totals)]

    bars1 = ax.bar([i - w/2 for i in x], hot_err_pct, w, label="Hot Room", color=colors_hot, alpha=0.85)
    bars2 = ax.bar([i + w/2 for i in x], multi_err_pct, w, label="Multi Room", color=colors_multi, alpha=0.85)

    ax.set_xticks(list(x))
    ax.set_xticklabels([ep.split(" ")[1] for ep in endpoints], rotation=15, ha="right", fontsize=9)
    ax.set_ylabel("Error Rate (%)")
    ax.set_title("Experiment 2: Error Rate — Hot Room vs Multi Room\n(DynamoDB throttling manifests as 500 errors)")
    ax.legend()

    for bars in [bars1, bars2]:
        for b in bars:
            h = b.get_height()
            if h > 0.01:
                ax.text(b.get_x() + b.get_width()/2, h, f"{h:.2f}%",
                        ha="center", va="bottom", fontsize=8)

    fig.tight_layout()
    p = out / "exp2_error_rate.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 4: Aggregate summary bar chart
    # =========================================================================
    fig, axes = plt.subplots(1, 3, figsize=(12, 4))

    # Total throughput
    ax = axes[0]
    vals = [fnum(hot_agg, "Requests/s"), fnum(multi_agg, "Requests/s")]
    bars = ax.bar(["Hot Room", "Multi Room"], vals, color=[colors_hot, colors_multi], alpha=0.85)
    for b, v in zip(bars, vals):
        ax.text(b.get_x() + b.get_width()/2, v, f"{v:.1f}", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Total Requests/s")
    ax.set_title("Aggregate Throughput")

    # Average latency
    ax = axes[1]
    vals = [fnum(hot_agg, "Average Response Time"), fnum(multi_agg, "Average Response Time")]
    bars = ax.bar(["Hot Room", "Multi Room"], vals, color=[colors_hot, colors_multi], alpha=0.85)
    for b, v in zip(bars, vals):
        ax.text(b.get_x() + b.get_width()/2, v, f"{v:.0f}ms", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Avg Latency (ms)")
    ax.set_title("Aggregate Latency")

    # Total error rate
    ax = axes[2]
    hot_total_err = fnum(hot_agg, "Failure Count")
    hot_total_req = fnum(hot_agg, "Request Count")
    multi_total_err = fnum(multi_agg, "Failure Count")
    multi_total_req = fnum(multi_agg, "Request Count")
    vals = [
        100.0 * hot_total_err / hot_total_req if hot_total_req > 0 else 0,
        100.0 * multi_total_err / multi_total_req if multi_total_req > 0 else 0,
    ]
    bars = ax.bar(["Hot Room", "Multi Room"], vals, color=[colors_hot, colors_multi], alpha=0.85)
    for b, v in zip(bars, vals):
        ax.text(b.get_x() + b.get_width()/2, v, f"{v:.2f}%", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Error Rate (%)")
    ax.set_title("Aggregate Error Rate")

    fig.suptitle("Experiment 2: Overall Comparison", fontsize=12, y=1.02)
    fig.tight_layout()
    p = out / "exp2_summary.png"
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 5 (optional): Time-series throughput
    # =========================================================================
    if args.hot_history and args.multi_history:
        hot_hist = load_history(args.hot_history)
        multi_hist = load_history(args.multi_history)

        fig, ax = plt.subplots(figsize=(12, 5))

        # Filter to "Aggregated" rows and extract timestamp + rps
        def extract_ts_rps(hist):
            ts, rps = [], []
            t0 = None
            for row in hist:
                if row.get("Name", "").strip() != "Aggregated":
                    continue
                t = fnum(row, "Timestamp")
                r = fnum(row, "Requests/s")
                if t0 is None:
                    t0 = t
                ts.append((t - t0))  # seconds from start
                rps.append(r)
            return ts, rps

        hot_ts, hot_rps_ts = extract_ts_rps(hot_hist)
        multi_ts, multi_rps_ts = extract_ts_rps(multi_hist)

        ax.plot(hot_ts, hot_rps_ts, color=colors_hot, label="Hot Room", alpha=0.8, linewidth=1.5)
        ax.plot(multi_ts, multi_rps_ts, color=colors_multi, label="Multi Room", alpha=0.8, linewidth=1.5)

        ax.set_xlabel("Time (seconds)")
        ax.set_ylabel("Requests/s")
        ax.set_title("Experiment 2: Throughput Over Time")
        ax.legend()
        ax.grid(True, alpha=0.3)

        fig.tight_layout()
        p = out / "exp2_throughput_timeseries.png"
        fig.savefig(p, dpi=150)
        plt.close(fig)
        print(f"Wrote {p}")

    # =========================================================================
    # Print summary table
    # =========================================================================
    print("\n" + "=" * 65)
    print("  EXPERIMENT 2 SUMMARY")
    print("=" * 65)
    print(f"  {'Metric':<30} {'Hot Room':>12} {'Multi Room':>12}")
    print("-" * 65)
    print(f"  {'Total Requests/s':<30} {fnum(hot_agg, 'Requests/s'):>12.1f} {fnum(multi_agg, 'Requests/s'):>12.1f}")
    print(f"  {'Avg Latency (ms)':<30} {fnum(hot_agg, 'Average Response Time'):>12.0f} {fnum(multi_agg, 'Average Response Time'):>12.0f}")
    print(f"  {'p95 Latency (ms)':<30} {fnum(hot_agg, '95%'):>12.0f} {fnum(multi_agg, '95%'):>12.0f}")
    print(f"  {'p99 Latency (ms)':<30} {fnum(hot_agg, '99%'):>12.0f} {fnum(multi_agg, '99%'):>12.0f}")
    print(f"  {'Error Rate (%)':<30} {vals[0]:>11.2f}% {vals[1]:>11.2f}%")
    print(f"  {'Total Requests':<30} {fnum(hot_agg, 'Request Count'):>12.0f} {fnum(multi_agg, 'Request Count'):>12.0f}")
    print(f"  {'Total Failures':<30} {fnum(hot_agg, 'Failure Count'):>12.0f} {fnum(multi_agg, 'Failure Count'):>12.0f}")
    print("=" * 65)


if __name__ == "__main__":
    main()

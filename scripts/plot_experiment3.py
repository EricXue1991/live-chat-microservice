#!/usr/bin/env python3
"""
Experiment 3 — Sync vs Async reactions (Locust) chart generator.

Usage (paths from run_experiment3_aws.sh output):
    python scripts/plot_experiment3.py \\
      --async-csv scripts/results/exp3_YYYYMMDD_HHMMSS/async/locust_stats.csv \\
      --sync-csv  scripts/results/exp3_YYYYMMDD_HHMMSS/sync/locust_stats.csv \\
      --out-dir report/figures/exp3_aws

Or:
    python scripts/plot_experiment3.py --results-dir scripts/results/exp3_YYYYMMDD_HHMMSS

Output: PNGs under --out-dir (default: report/figures/exp3_aws)
"""

from __future__ import annotations

import argparse
import csv
from pathlib import Path


def load_aggregated(csv_path: Path) -> dict:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Name", "").strip() == "Aggregated":
                return row
    return {}


def load_row_by_name(csv_path: Path, name_substr: str) -> dict:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            n = row.get("Name", "").strip()
            if name_substr in n:
                return row
    return {}


def load_history(csv_path: Path) -> list[dict]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def fnum(row: dict, key: str, default: float = 0.0) -> float:
    try:
        return float(row.get(key, default))
    except (ValueError, TypeError):
        return default


def error_rate_pct(agg: dict) -> float:
    total = fnum(agg, "Request Count")
    fails = fnum(agg, "Failure Count")
    return 100.0 * fails / total if total > 0 else 0.0


def main() -> None:
    ap = argparse.ArgumentParser(description="Plot Experiment 3 (async vs sync reactions).")
    ap.add_argument("--async-csv", type=Path, default=None)
    ap.add_argument("--sync-csv", type=Path, default=None)
    ap.add_argument("--results-dir", type=Path, default=None,
                    help="Directory containing async/ and sync/ with locust_stats.csv")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp3_aws")
    args = ap.parse_args()

    async_csv = args.async_csv
    sync_csv = args.sync_csv
    if args.results_dir:
        async_csv = args.results_dir / "async" / "locust_stats.csv"
        sync_csv = args.results_dir / "sync" / "locust_stats.csv"

    if not async_csv or not sync_csv:
        raise SystemExit("Provide --results-dir OR both --async-csv and --sync-csv.")
    if not async_csv.is_file():
        raise SystemExit(f"Missing: {async_csv}")
    if not sync_csv.is_file():
        raise SystemExit(f"Missing: {sync_csv}")

    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("Install matplotlib:  pip install matplotlib")

    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)

    labels_mode = ["Async", "Sync"]
    paths = [async_csv, sync_csv]
    aggs = [load_aggregated(p) for p in paths]
    if not any(aggs):
        raise SystemExit("Could not find 'Aggregated' row in CSV(s).")

    # Focus endpoint: POST reactions (same label as Locust task names in this project)
    react_post = "/api/reactions [POST]"
    ep_async = load_row_by_name(async_csv, react_post)
    ep_sync = load_row_by_name(sync_csv, react_post)

    colors = {"async": "#27ae60", "sync": "#e74c3c"}
    c_list = [colors["async"], colors["sync"]]

    # --- 1) Aggregate throughput & latency ---
    fig, axes = plt.subplots(1, 2, figsize=(10, 4.5))

    rps = [fnum(a, "Requests/s") for a in aggs]
    ax = axes[0]
    bars = ax.bar(labels_mode, rps, color=c_list, alpha=0.88, edgecolor="white", linewidth=1.2)
    for b, v in zip(bars, rps):
        ax.text(b.get_x() + b.get_width() / 2, v, f"{v:.1f}",
                ha="center", va="bottom", fontsize=10, fontweight="bold")
    ax.set_ylabel("Requests/s (Aggregated)")
    ax.set_title("Experiment 3: Total Throughput")
    ax.grid(axis="y", alpha=0.3)

    avg_lat = [fnum(a, "Average Response Time") for a in aggs]
    p95_lat = [fnum(a, "95%") for a in aggs]
    p99_lat = [fnum(a, "99%") for a in aggs]

    ax = axes[1]
    x = range(len(labels_mode))
    w = 0.25
    ax.bar([i - w for i in x], avg_lat, width=w, label="Avg", color="#3498db", alpha=0.9)
    ax.bar(x, p95_lat, width=w, label="p95", color="#f39c12", alpha=0.9)
    ax.bar([i + w for i in x], p99_lat, width=w, label="p99", color="#9b59b6", alpha=0.9)
    ax.set_xticks(list(x))
    ax.set_xticklabels(labels_mode)
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Experiment 3: Aggregate Latency")
    ax.legend()
    ax.grid(axis="y", alpha=0.3)

    fig.suptitle("Sync vs Async — same Locust profile (AWS ECS)", fontsize=11, y=1.02)
    fig.tight_layout()
    p = out / "exp3_summary_throughput_latency.png"
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print(f"Wrote {p}")

    # --- 2) Error rate ---
    err_pct = [error_rate_pct(a) for a in aggs]
    fig, ax = plt.subplots(figsize=(6, 4.5))
    bars = ax.bar(labels_mode, err_pct, color=c_list, alpha=0.88)
    for b, v in zip(bars, err_pct):
        ax.text(b.get_x() + b.get_width() / 2, v, f"{v:.2f}%",
                ha="center", va="bottom", fontsize=10, fontweight="bold")
    ax.set_ylabel("Error rate (%)")
    ax.set_title("Experiment 3: Failure rate (Aggregated)")
    ax.set_ylim(0, max(5.0, max(err_pct) * 1.25) if err_pct else 1.0)
    ax.grid(axis="y", alpha=0.3)
    fig.tight_layout()
    p = out / "exp3_error_rate.png"
    fig.savefig(p, dpi=150, bbox_inches="tight")
    plt.close(fig)
    print(f"Wrote {p}")

    # --- 3) POST /api/reactions only (if row exists) ---
    if ep_async or ep_sync:
        fig, axes = plt.subplots(1, 2, figsize=(10, 4.5))
        rps_ep = [fnum(ep_async, "Requests/s"), fnum(ep_sync, "Requests/s")]
        ax = axes[0]
        bars = ax.bar(labels_mode, rps_ep, color=c_list, alpha=0.88)
        for b, v in zip(bars, rps_ep):
            ax.text(b.get_x() + b.get_width() / 2, v, f"{v:.1f}",
                    ha="center", va="bottom", fontsize=10, fontweight="bold")
        ax.set_ylabel("Requests/s")
        ax.set_title(f"Throughput — {react_post}")
        ax.grid(axis="y", alpha=0.3)

        err_ep = [
            error_rate_pct(ep_async) if ep_async else 0.0,
            error_rate_pct(ep_sync) if ep_sync else 0.0,
        ]
        ax = axes[1]
        bars = ax.bar(labels_mode, err_ep, color=c_list, alpha=0.88)
        for b, v in zip(bars, err_ep):
            ax.text(b.get_x() + b.get_width() / 2, v, f"{v:.2f}%",
                    ha="center", va="bottom", fontsize=10, fontweight="bold")
        ax.set_ylabel("Error rate (%)")
        ax.set_title(f"Failures — {react_post}")
        ax.set_ylim(0, max(5.0, max(err_ep) * 1.25) if err_ep else 1.0)
        ax.grid(axis="y", alpha=0.3)

        fig.suptitle("Reaction-heavy path (POST)", fontsize=11, y=1.02)
        fig.tight_layout()
        p = out / "exp3_reactions_post.png"
        fig.savefig(p, dpi=150, bbox_inches="tight")
        plt.close(fig)
        print(f"Wrote {p}")

    # --- 4) Time series (Aggregated RPS) ---
    hist_async = async_csv.parent / "locust_stats_history.csv"
    hist_sync = sync_csv.parent / "locust_stats_history.csv"
    if hist_async.is_file() and hist_sync.is_file():
        fig, ax = plt.subplots(figsize=(11, 4.5))

        def series(hist_path: Path, label: str, color: str) -> None:
            hist = load_history(hist_path)
            ts, rps_ts = [], []
            t0 = None
            for row in hist:
                if row.get("Name", "").strip() != "Aggregated":
                    continue
                t = fnum(row, "Timestamp")
                r = fnum(row, "Requests/s")
                if t0 is None:
                    t0 = t
                ts.append(t - t0)
                rps_ts.append(r)
            ax.plot(ts, rps_ts, color=color, label=label, alpha=0.85, linewidth=1.4)

        series(hist_async, "Async", colors["async"])
        series(hist_sync, "Sync", colors["sync"])

        ax.set_xlabel("Time (s)")
        ax.set_ylabel("Requests/s (Aggregated)")
        ax.set_title("Experiment 3: Throughput over time")
        ax.legend()
        ax.grid(True, alpha=0.3)
        fig.tight_layout()
        p = out / "exp3_throughput_timeseries.png"
        fig.savefig(p, dpi=150, bbox_inches="tight")
        plt.close(fig)
        print(f"Wrote {p}")
    else:
        print("  (Skipping time series: locust_stats_history.csv not found next to stats CSV.)")

    # --- Summary table ---
    print("\n" + "=" * 72)
    print("  EXPERIMENT 3 — Async vs Sync (Locust Aggregated)")
    print("=" * 72)
    print(f"  {'Mode':<8} {'RPS':>10} {'Avg(ms)':>10} {'p95':>8} {'p99':>8} {'Err%':>8}")
    print("-" * 72)
    for lab, a in zip(labels_mode, aggs):
        print(f"  {lab:<8} {fnum(a, 'Requests/s'):>10.1f} "
              f"{fnum(a, 'Average Response Time'):>10.0f} {fnum(a, '95%'):>8.0f} "
              f"{fnum(a, '99%'):>8.0f} {error_rate_pct(a):>7.2f}%")
    print("=" * 72)


if __name__ == "__main__":
    main()

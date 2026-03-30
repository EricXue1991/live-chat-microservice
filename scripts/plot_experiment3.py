#!/usr/bin/env python3
"""
Experiment 3 charts from two Locust locust_stats.csv files (sync vs async).

  pip install matplotlib
  python scripts/plot_experiment3.py \
    --sync-csv scripts/results/exp3_xxx_sync/locust_stats.csv \
    --async-csv scripts/results/exp3_xxx_async/locust_stats.csv

PNG output: report/figures/exp3/ (created if missing)
"""
from __future__ import annotations

import argparse
import csv
from pathlib import Path


def reaction_post_row(csv_path: Path) -> dict[str, str]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Type") == "POST" and row.get("Name") == "/api/reactions [POST]":
                return row
    raise SystemExit(f"No POST /api/reactions row in {csv_path}")


def fnum(row: dict[str, str], key: str) -> float:
    return float(row[key].strip())


def main() -> None:
    ap = argparse.ArgumentParser(description="Plot Experiment 3 bar charts from Locust CSVs.")
    ap.add_argument("--sync-csv", type=Path, required=True, dest="sync_csv", help="sync run locust_stats.csv")
    ap.add_argument("--async-csv", type=Path, required=True, dest="async_csv", help="async run locust_stats.csv")
    ap.add_argument(
        "--out-dir",
        type=Path,
        default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp3",
        help="directory for PNG files",
    )
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError as e:
        raise SystemExit("Need matplotlib: pip install matplotlib") from e

    row_s = reaction_post_row(args.sync_csv)
    row_a = reaction_post_row(args.async_csv)

    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)

    # --- Throughput ---
    fig, ax = plt.subplots(figsize=(5, 4))
    labels = ["sync", "async"]
    xs = range(len(labels))
    rps = [fnum(row_s, "Requests/s"), fnum(row_a, "Requests/s")]
    bars = ax.bar(xs, rps, color=["#c44e52", "#4c72b0"])
    ax.set_xticks(list(xs))
    ax.set_xticklabels(labels)
    ax.set_ylabel("Requests/s (POST /api/reactions)")
    ax.set_title("Experiment 3: reaction POST throughput")
    for b, v in zip(bars, rps):
        ax.text(b.get_x() + b.get_width() / 2, b.get_height(), f"{v:.1f}", ha="center", va="bottom", fontsize=10)
    fig.tight_layout()
    p1 = out / "exp3_reaction_throughput.png"
    fig.savefig(p1, dpi=150)
    plt.close(fig)

    # --- Latency percentiles (grouped) ---
    metrics = [("p50", "50%"), ("p95", "95%"), ("p99", "99%")]
    fig, ax = plt.subplots(figsize=(7, 4))
    w = 0.35
    x = [i for i in range(len(metrics))]
    sync_vals = [fnum(row_s, key) for _, key in metrics]
    async_vals = [fnum(row_a, key) for _, key in metrics]
    ax.bar([i - w / 2 for i in x], sync_vals, width=w, label="sync", color="#c44e52")
    ax.bar([i + w / 2 for i in x], async_vals, width=w, label="async", color="#4c72b0")
    ax.set_xticks(x)
    ax.set_xticklabels([m[0] for m in metrics])
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Experiment 3: reaction POST latency (Locust)")
    ax.legend()
    fig.tight_layout()
    p2 = out / "exp3_reaction_latency.png"
    fig.savefig(p2, dpi=150)
    plt.close(fig)

    print(f"Wrote {p1}")
    print(f"Wrote {p2}")


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""
Experiment 2 charts: Hot Room vs Multi-Room (two Locust locust_stats.csv files).

  python scripts/plot_experiment2.py \
    --hot-csv  scripts/results/exp2_xxx_hot/locust_stats.csv \
    --dist-csv scripts/results/exp2_xxx_distributed/locust_stats.csv

PNG output: report/figures/exp2/
"""
from __future__ import annotations
import argparse, csv
from pathlib import Path


def msg_post_row(csv_path: Path) -> dict[str, str]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Type") == "POST" and "messages" in row.get("Name", ""):
                return row
    raise SystemExit(f"No POST /api/messages row in {csv_path}")


def fnum(row: dict, key: str) -> float:
    return float(row[key].strip())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--hot-csv",  type=Path, required=True, dest="hot_csv")
    ap.add_argument("--dist-csv", type=Path, required=True, dest="dist_csv")
    ap.add_argument("--out-dir",  type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp2")
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("pip install matplotlib")

    row_h = msg_post_row(args.hot_csv)
    row_d = msg_post_row(args.dist_csv)
    args.out_dir.mkdir(parents=True, exist_ok=True)

    # --- Throughput ---
    fig, ax = plt.subplots(figsize=(5, 4))
    labels = ["Hot Room\n(90% one room)", "Distributed\n(10% one room)"]
    rps = [fnum(row_h, "Requests/s"), fnum(row_d, "Requests/s")]
    bars = ax.bar(labels, rps, color=["#c44e52", "#4c72b0"])
    for b, v in zip(bars, rps):
        ax.text(b.get_x() + b.get_width()/2, b.get_height() + 0.3,
                f"{v:.1f}", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Requests/s (POST /api/messages)")
    ax.set_title("Experiment 2: Hot Room vs Distributed — throughput")
    fig.tight_layout()
    p1 = args.out_dir / "exp2_throughput.png"
    fig.savefig(p1, dpi=150); plt.close(fig)

    # --- Latency percentiles ---
    metrics = [("p50", "50%"), ("p95", "95%"), ("p99", "99%")]
    hot_vals  = [fnum(row_h, k) for _, k in metrics]
    dist_vals = [fnum(row_d, k) for _, k in metrics]
    fig, ax = plt.subplots(figsize=(7, 4))
    w = 0.35
    x = list(range(len(metrics)))
    b1 = ax.bar([i - w/2 for i in x], hot_vals,  width=w, label="Hot Room",    color="#c44e52")
    b2 = ax.bar([i + w/2 for i in x], dist_vals, width=w, label="Distributed", color="#4c72b0")
    for b, v in zip(b1, hot_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+5, f"{v:.0f}", ha="center", va="bottom", fontsize=8)
    for b, v in zip(b2, dist_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+5, f"{v:.0f}", ha="center", va="bottom", fontsize=8)
    ax.set_xticks(x); ax.set_xticklabels([m[0] for m in metrics])
    ax.set_ylabel("Latency (ms)"); ax.set_title("Experiment 2: Hot Room vs Distributed — latency")
    ax.legend(); fig.tight_layout()
    p2 = args.out_dir / "exp2_latency.png"
    fig.savefig(p2, dpi=150); plt.close(fig)

    print(f"=== Experiment 2 Results ===")
    print(f"{'Metric':<8} {'Hot Room':>12} {'Distributed':>14} {'Diff':>8}")
    print("-" * 46)
    for label, hv, dv in zip(["p50","p95","p99"], hot_vals, dist_vals):
        print(f"{label:<8} {hv:>10.0f}ms {dv:>12.0f}ms {dv-hv:>+6.0f}ms")
    print(f"\nWrote {p1}\nWrote {p2}")


if __name__ == "__main__":
    main()

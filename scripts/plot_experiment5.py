#!/usr/bin/env python3
"""
Experiment 5 charts: Cache Hit vs Miss (two Locust locust_stats.csv files).

  python scripts/plot_experiment5.py \
    --cache-on-csv  scripts/results/exp5_xxx_cache_on/locust_stats.csv \
    --cache-off-csv scripts/results/exp5_xxx_cache_off/locust_stats.csv

PNG output: report/figures/exp5/
"""
from __future__ import annotations
import argparse, csv
from pathlib import Path


def msg_get_row(csv_path: Path) -> dict[str, str]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Type") == "GET" and "messages" in row.get("Name", ""):
                return row
    raise SystemExit(f"No GET /api/messages row in {csv_path}")


def fnum(row: dict, key: str) -> float:
    return float(row[key].strip())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--cache-on-csv",  type=Path, required=True, dest="on_csv")
    ap.add_argument("--cache-off-csv", type=Path, required=True, dest="off_csv")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp5")
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("pip install matplotlib")

    row_on  = msg_get_row(args.on_csv)
    row_off = msg_get_row(args.off_csv)
    args.out_dir.mkdir(parents=True, exist_ok=True)

    # --- Latency percentiles ---
    metrics = [("p50", "50%"), ("p95", "95%"), ("p99", "99%")]
    on_vals  = [fnum(row_on,  k) for _, k in metrics]
    off_vals = [fnum(row_off, k) for _, k in metrics]

    fig, ax = plt.subplots(figsize=(7, 4))
    w = 0.35
    x = list(range(len(metrics)))
    b1 = ax.bar([i - w/2 for i in x], on_vals,  width=w, label="Cache ON (Redis)",  color="#4c72b0")
    b2 = ax.bar([i + w/2 for i in x], off_vals, width=w, label="Cache OFF (DynamoDB)", color="#c44e52")
    for b, v in zip(b1, on_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+2, f"{v:.0f}ms", ha="center", va="bottom", fontsize=8)
    for b, v in zip(b2, off_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+2, f"{v:.0f}ms", ha="center", va="bottom", fontsize=8)
    ax.set_xticks(x); ax.set_xticklabels([m[0] for m in metrics])
    ax.set_ylabel("Latency (ms) — GET /api/messages")
    ax.set_title("Experiment 5: Cache Hit vs Miss — read latency")
    ax.legend(); fig.tight_layout()
    p1 = args.out_dir / "exp5_read_latency.png"
    fig.savefig(p1, dpi=150); plt.close(fig)

    # --- Average latency bar ---
    avg_on  = fnum(row_on,  "Average Response Time")
    avg_off = fnum(row_off, "Average Response Time")
    fig, ax = plt.subplots(figsize=(5, 4))
    bars = ax.bar(["Cache ON", "Cache OFF"], [avg_on, avg_off], color=["#4c72b0", "#c44e52"])
    for b, v in zip(bars, [avg_on, avg_off]):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+1, f"{v:.0f}ms", ha="center", va="bottom", fontsize=11)
    ax.set_ylabel("Average read latency (ms)")
    ax.set_title("Experiment 5: Cache Hit vs Miss — average")
    fig.tight_layout()
    p2 = args.out_dir / "exp5_avg_latency.png"
    fig.savefig(p2, dpi=150); plt.close(fig)

    print(f"=== Experiment 5 Results (GET /api/messages) ===")
    print(f"{'Metric':<8} {'Cache ON':>12} {'Cache OFF':>12} {'Speedup':>10}")
    print("-" * 46)
    for label, ov, fv in zip(["p50","p95","p99"], on_vals, off_vals):
        sp = fv/ov if ov > 0 else 0
        print(f"{label:<8} {ov:>10.0f}ms {fv:>10.0f}ms {sp:>9.1f}x")
    sp_avg = avg_off/avg_on if avg_on > 0 else 0
    print(f"{'average':<8} {avg_on:>10.0f}ms {avg_off:>10.0f}ms {sp_avg:>9.1f}x")
    print(f"\nWrote {p1}\nWrote {p2}")


if __name__ == "__main__":
    main()

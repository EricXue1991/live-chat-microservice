#!/usr/bin/env python3
"""
Experiment 6 charts: Rate Limiting ON vs OFF (two Locust locust_stats.csv files).

  python scripts/plot_experiment6.py \
    --limited-csv   scripts/results/exp6_xxx_limited/locust_stats.csv \
    --unlimited-csv scripts/results/exp6_xxx_unlimited/locust_stats.csv

PNG output: report/figures/exp6/
"""
from __future__ import annotations
import argparse, csv
from pathlib import Path


def aggregated_row(csv_path: Path) -> dict[str, str]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            if row.get("Name") == "Aggregated":
                return row
    raise SystemExit(f"No Aggregated row in {csv_path}")


def fnum(row: dict, key: str) -> float:
    return float(row[key].strip())


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--limited-csv",   type=Path, required=True, dest="lim_csv")
    ap.add_argument("--unlimited-csv", type=Path, required=True, dest="unlim_csv")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp6")
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("pip install matplotlib")

    row_l = aggregated_row(args.lim_csv)
    row_u = aggregated_row(args.unlim_csv)
    args.out_dir.mkdir(parents=True, exist_ok=True)

    # --- Throughput comparison ---
    rps_l = fnum(row_l, "Requests/s")
    rps_u = fnum(row_u, "Requests/s")
    fig, ax = plt.subplots(figsize=(5, 4))
    bars = ax.bar(["Rate Limited\n(20 RPS/user)", "No Limit"], [rps_l, rps_u],
                  color=["#c44e52", "#4c72b0"])
    for b, v in zip(bars, [rps_l, rps_u]):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+0.3,
                f"{v:.1f}", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Total Requests/s"); ax.set_title("Experiment 6: Rate Limiting — throughput")
    fig.tight_layout()
    p1 = args.out_dir / "exp6_throughput.png"
    fig.savefig(p1, dpi=150); plt.close(fig)

    # --- Error rate comparison ---
    err_l = fnum(row_l, "Failures/s")
    err_u = fnum(row_u, "Failures/s")
    fig, ax = plt.subplots(figsize=(5, 4))
    bars = ax.bar(["Rate Limited\n(20 RPS/user)", "No Limit"], [err_l, err_u],
                  color=["#c44e52", "#4c72b0"])
    for b, v in zip(bars, [err_l, err_u]):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+0.05,
                f"{v:.2f}", ha="center", va="bottom", fontsize=10)
    ax.set_ylabel("Failures/s (429 Too Many Requests)")
    ax.set_title("Experiment 6: Rate Limiting — error rate")
    fig.tight_layout()
    p2 = args.out_dir / "exp6_errors.png"
    fig.savefig(p2, dpi=150); plt.close(fig)

    # --- Latency percentiles ---
    metrics = [("p50", "50%"), ("p95", "95%"), ("p99", "99%")]
    lim_vals   = [fnum(row_l, k) for _, k in metrics]
    unlim_vals = [fnum(row_u, k) for _, k in metrics]
    fig, ax = plt.subplots(figsize=(7, 4))
    w = 0.35; x = list(range(len(metrics)))
    b1 = ax.bar([i-w/2 for i in x], lim_vals,   width=w, label="Rate Limited", color="#c44e52")
    b2 = ax.bar([i+w/2 for i in x], unlim_vals, width=w, label="No Limit",     color="#4c72b0")
    for b, v in zip(b1, lim_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+3, f"{v:.0f}", ha="center", va="bottom", fontsize=8)
    for b, v in zip(b2, unlim_vals):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+3, f"{v:.0f}", ha="center", va="bottom", fontsize=8)
    ax.set_xticks(x); ax.set_xticklabels([m[0] for m in metrics])
    ax.set_ylabel("Latency (ms)"); ax.set_title("Experiment 6: Rate Limiting — latency")
    ax.legend(); fig.tight_layout()
    p3 = args.out_dir / "exp6_latency.png"
    fig.savefig(p3, dpi=150); plt.close(fig)

    print(f"=== Experiment 6 Results (Aggregated) ===")
    print(f"{'Metric':<12} {'Rate Limited':>14} {'No Limit':>12}")
    print("-" * 42)
    print(f"{'Requests/s':<12} {rps_l:>12.1f}   {rps_u:>10.1f}")
    print(f"{'Failures/s':<12} {err_l:>12.2f}   {err_u:>10.2f}")
    for label, lv, uv in zip(["p50","p95","p99"], lim_vals, unlim_vals):
        print(f"{label:<12} {lv:>12.0f}ms {uv:>10.0f}ms")
    print(f"\nWrote {p1}\nWrote {p2}\nWrote {p3}")


if __name__ == "__main__":
    main()

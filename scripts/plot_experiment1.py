#!/usr/bin/env python3
"""
Experiment 1 charts: Scale-Out (1 / 2 / 3 backend replicas).

  python scripts/plot_experiment1.py \
    --r1-csv scripts/results/exp1_xxx_r1/locust_stats.csv \
    --r2-csv scripts/results/exp1_xxx_r2/locust_stats.csv \
    --r3-csv scripts/results/exp1_xxx_r3/locust_stats.csv

PNG output: report/figures/exp1/
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
    ap.add_argument("--r1-csv", type=Path, required=True, dest="r1")
    ap.add_argument("--r2-csv", type=Path, required=True, dest="r2")
    ap.add_argument("--r3-csv", type=Path, required=True, dest="r3")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp1")
    args = ap.parse_args()

    try:
        import matplotlib.pyplot as plt
    except ImportError:
        raise SystemExit("pip install matplotlib")

    rows = [aggregated_row(p) for p in [args.r1, args.r2, args.r3]]
    args.out_dir.mkdir(parents=True, exist_ok=True)

    replicas = [1, 2, 3]
    rps      = [fnum(r, "Requests/s") for r in rows]
    p50      = [fnum(r, "50%")        for r in rows]
    p95      = [fnum(r, "95%")        for r in rows]
    p99      = [fnum(r, "99%")        for r in rows]
    ideal    = [rps[0] * n for n in replicas]

    # --- Throughput: actual vs ideal linear scaling ---
    fig, ax = plt.subplots(figsize=(6, 4))
    ax.plot(replicas, rps,   "o-", color="#4c72b0", label="Actual",       linewidth=2, markersize=8)
    ax.plot(replicas, ideal, "s--", color="#c44e52", label="Ideal linear", linewidth=1.5, markersize=6)
    for x, v in zip(replicas, rps):
        ax.text(x, v + 0.5, f"{v:.1f}", ha="center", va="bottom", fontsize=9)
    ax.set_xlabel("Backend replicas")
    ax.set_ylabel("Total Requests/s")
    ax.set_title("Experiment 1: Scale-Out — throughput")
    ax.set_xticks(replicas)
    ax.legend()
    fig.tight_layout()
    p1 = args.out_dir / "exp1_throughput.png"
    fig.savefig(p1, dpi=150); plt.close(fig)

    # --- Latency percentiles vs replica count ---
    fig, ax = plt.subplots(figsize=(6, 4))
    ax.plot(replicas, p50, "o-",  label="p50", color="#4c72b0", linewidth=2, markersize=8)
    ax.plot(replicas, p95, "s-",  label="p95", color="#dd8452", linewidth=2, markersize=8)
    ax.plot(replicas, p99, "^-",  label="p99", color="#c44e52", linewidth=2, markersize=8)
    ax.set_xlabel("Backend replicas")
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Experiment 1: Scale-Out — latency")
    ax.set_xticks(replicas)
    ax.legend()
    fig.tight_layout()
    p2 = args.out_dir / "exp1_latency.png"
    fig.savefig(p2, dpi=150); plt.close(fig)

    # --- Scaling efficiency bar ---
    efficiency = [v / ideal[i] * 100 for i, v in enumerate(rps)]
    fig, ax = plt.subplots(figsize=(5, 4))
    bars = ax.bar([f"{n} replica{'s' if n>1 else ''}" for n in replicas],
                  efficiency, color=["#4c72b0", "#55a868", "#dd8452"])
    for b, v in zip(bars, efficiency):
        ax.text(b.get_x()+b.get_width()/2, b.get_height()+0.5,
                f"{v:.0f}%", ha="center", va="bottom", fontsize=10)
    ax.axhline(100, color="gray", linestyle="--", linewidth=1)
    ax.set_ylabel("Scaling efficiency (actual / ideal × 100%)")
    ax.set_title("Experiment 1: Scale-Out — efficiency")
    ax.set_ylim(0, 120)
    fig.tight_layout()
    p3 = args.out_dir / "exp1_efficiency.png"
    fig.savefig(p3, dpi=150); plt.close(fig)

    print(f"=== Experiment 1 Results (Aggregated) ===")
    print(f"{'Replicas':<10} {'RPS':>8} {'Ideal':>8} {'Efficiency':>12} {'p50':>8} {'p95':>8} {'p99':>8}")
    print("-" * 68)
    for n, rv, iv, p5, p9, p99v, eff in zip(replicas, rps, ideal, p50, p95, p99, efficiency):
        print(f"{n:<10} {rv:>8.1f} {iv:>8.1f} {eff:>11.0f}% {p5:>7.0f}ms {p9:>7.0f}ms {p99v:>7.0f}ms")
    print(f"\nWrote {p1}\nWrote {p2}\nWrote {p3}")


if __name__ == "__main__":
    main()

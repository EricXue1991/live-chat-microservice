#!/usr/bin/env python3
"""
Experiment 1 — Scale-Out (1/2/4/8 replicas) chart generator.

Usage:
    python scripts/plot_experiment1.py --results-dir scripts/results/exp1_xxx

Output: report/figures/exp1/  (PNG files)
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


def load_endpoint_rows(csv_path: Path) -> dict[str, dict]:
    rows = {}
    with csv_path.open(newline="", encoding="utf-8") as f:
        for row in csv.DictReader(f):
            name = row.get("Name", "").strip()
            if name and name != "Aggregated":
                rows[name] = row
    return rows


def load_history(csv_path: Path) -> list[dict]:
    with csv_path.open(newline="", encoding="utf-8") as f:
        return list(csv.DictReader(f))


def fnum(row: dict, key: str, default: float = 0.0) -> float:
    try:
        return float(row.get(key, default))
    except (ValueError, TypeError):
        return default


def main():
    ap = argparse.ArgumentParser(description="Plot Experiment 1 charts.")
    ap.add_argument("--results-dir", type=Path, required=True, dest="results_dir")
    ap.add_argument("--out-dir", type=Path,
                    default=Path(__file__).resolve().parent.parent / "report" / "figures" / "exp1")
    args = ap.parse_args()

    try:
        import matplotlib
        matplotlib.use("Agg")
        import matplotlib.pyplot as plt
        import matplotlib.ticker as ticker
    except ImportError:
        raise SystemExit("Install matplotlib:  pip install matplotlib")

    out = args.out_dir
    out.mkdir(parents=True, exist_ok=True)

    # Discover replica dirs
    replica_dirs = sorted(args.results_dir.glob("replicas_*"),
                          key=lambda p: int(p.name.split("_")[1]))
    if not replica_dirs:
        raise SystemExit(f"No replicas_N dirs found in {args.results_dir}")

    replicas = []
    aggs = []
    endpoint_data = []

    for d in replica_dirs:
        n = int(d.name.split("_")[1])
        csv_file = d / "locust_stats.csv"
        if not csv_file.exists():
            print(f"  WARNING: {csv_file} not found, skipping replicas={n}")
            continue
        replicas.append(n)
        aggs.append(load_aggregated(csv_file))
        endpoint_data.append(load_endpoint_rows(csv_file))

    if not replicas:
        raise SystemExit("No valid results found.")

    print(f"Found results for replicas: {replicas}")

    colors = ["#e74c3c", "#f39c12", "#27ae60", "#3498db", "#9b59b6", "#1abc9c", "#e67e22", "#2c3e50"]

    # =========================================================================
    # Chart 1: Aggregate Throughput vs Replicas
    # =========================================================================
    fig, ax = plt.subplots(figsize=(8, 5))

    rps_vals = [fnum(a, "Requests/s") for a in aggs]
    bars = ax.bar([str(r) for r in replicas], rps_vals,
                  color=[colors[i % len(colors)] for i in range(len(replicas))], alpha=0.85)

    for b, v in zip(bars, rps_vals):
        ax.text(b.get_x() + b.get_width()/2, v, f"{v:.1f}",
                ha="center", va="bottom", fontsize=10, fontweight="bold")

    # Ideal linear scaling line
    if rps_vals[0] > 0:
        ideal = [rps_vals[0] * r / replicas[0] for r in replicas]
        ax.plot([str(r) for r in replicas], ideal, "k--", alpha=0.4, label="Ideal linear scaling")
        ax.legend()

    ax.set_xlabel("Number of Replicas")
    ax.set_ylabel("Total Requests/s")
    ax.set_title("Experiment 1: Throughput vs Replicas")
    ax.grid(axis="y", alpha=0.3)

    fig.tight_layout()
    p = out / "exp1_throughput_vs_replicas.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 2: Latency vs Replicas (avg, p95, p99)
    # =========================================================================
    fig, ax = plt.subplots(figsize=(8, 5))

    avg_lat = [fnum(a, "Average Response Time") for a in aggs]
    p95_lat = [fnum(a, "95%") for a in aggs]
    p99_lat = [fnum(a, "99%") for a in aggs]

    x_labels = [str(r) for r in replicas]
    ax.plot(x_labels, avg_lat, "o-", color=colors[0], label="Avg", linewidth=2, markersize=8)
    ax.plot(x_labels, p95_lat, "s-", color=colors[1], label="p95", linewidth=2, markersize=8)
    ax.plot(x_labels, p99_lat, "^-", color=colors[3], label="p99", linewidth=2, markersize=8)

    for vals, offset in [(avg_lat, -8), (p95_lat, 8), (p99_lat, 8)]:
        for i, v in enumerate(vals):
            ax.annotate(f"{v:.0f}", (x_labels[i], v), textcoords="offset points",
                        xytext=(0, offset), ha="center", fontsize=8)

    ax.set_xlabel("Number of Replicas")
    ax.set_ylabel("Latency (ms)")
    ax.set_title("Experiment 1: Latency vs Replicas")
    ax.legend()
    ax.grid(True, alpha=0.3)

    fig.tight_layout()
    p = out / "exp1_latency_vs_replicas.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 3: Error Rate vs Replicas
    # =========================================================================
    fig, ax = plt.subplots(figsize=(8, 5))

    err_rates = []
    for a in aggs:
        total = fnum(a, "Request Count")
        fails = fnum(a, "Failure Count")
        err_rates.append(100.0 * fails / total if total > 0 else 0)

    bars = ax.bar(x_labels, err_rates,
                  color=[colors[i % len(colors)] for i in range(len(replicas))], alpha=0.85)
    for b, v in zip(bars, err_rates):
        ax.text(b.get_x() + b.get_width()/2, v, f"{v:.2f}%",
                ha="center", va="bottom", fontsize=10)

    ax.set_xlabel("Number of Replicas")
    ax.set_ylabel("Error Rate (%)")
    ax.set_title("Experiment 1: Error Rate vs Replicas")
    ax.grid(axis="y", alpha=0.3)

    fig.tight_layout()
    p = out / "exp1_error_rate_vs_replicas.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 4: Per-endpoint throughput breakdown
    # =========================================================================
    endpoints = ["/api/messages [POST]", "/api/reactions [POST]",
                 "/api/messages [GET]", "/api/reactions [GET]"]
    ep_short = ["POST msg", "POST react", "GET msg", "GET react"]

    fig, ax = plt.subplots(figsize=(10, 5))
    width = 0.18
    x_pos = list(range(len(endpoints)))

    for idx, (n, ep_rows) in enumerate(zip(replicas, endpoint_data)):
        offsets = [p + (idx - len(replicas)/2 + 0.5) * width for p in x_pos]
        vals = [fnum(ep_rows.get(ep, {}), "Requests/s") for ep in endpoints]
        ax.bar(offsets, vals, width, label=f"{n} replicas",
               color=colors[idx % len(colors)], alpha=0.85)

    ax.set_xticks(x_pos)
    ax.set_xticklabels(ep_short)
    ax.set_ylabel("Requests/s")
    ax.set_title("Experiment 1: Per-Endpoint Throughput by Replica Count")
    ax.legend()
    ax.grid(axis="y", alpha=0.3)

    fig.tight_layout()
    p = out / "exp1_endpoint_throughput.png"
    fig.savefig(p, dpi=150)
    plt.close(fig)
    print(f"Wrote {p}")

    # =========================================================================
    # Chart 5: Scaling efficiency
    # =========================================================================
    if len(replicas) >= 2 and rps_vals[0] > 0:
        fig, ax = plt.subplots(figsize=(8, 5))

        base_rps = rps_vals[0]
        base_n = replicas[0]
        efficiency = [100.0 * (rps / base_rps) / (n / base_n) for rps, n in zip(rps_vals, replicas)]

        bars = ax.bar(x_labels, efficiency,
                      color=[colors[i % len(colors)] for i in range(len(replicas))], alpha=0.85)
        for b, v in zip(bars, efficiency):
            ax.text(b.get_x() + b.get_width()/2, v, f"{v:.1f}%",
                    ha="center", va="bottom", fontsize=10, fontweight="bold")

        ax.axhline(y=100, color="gray", linestyle="--", alpha=0.5, label="100% = perfect scaling")
        ax.set_xlabel("Number of Replicas")
        ax.set_ylabel("Scaling Efficiency (%)")
        ax.set_title("Experiment 1: Scaling Efficiency\n(100% = perfect linear scaling)")
        ax.legend()
        ax.grid(axis="y", alpha=0.3)
        ax.set_ylim(0, max(120, max(efficiency) + 10))

        fig.tight_layout()
        p = out / "exp1_scaling_efficiency.png"
        fig.savefig(p, dpi=150)
        plt.close(fig)
        print(f"Wrote {p}")

    # =========================================================================
    # Chart 6: Time-series throughput overlay
    # =========================================================================
    has_history = all((d / "locust_stats_history.csv").exists() for d in replica_dirs
                      if int(d.name.split("_")[1]) in replicas)
    if has_history:
        fig, ax = plt.subplots(figsize=(12, 5))
        for idx, (d, n) in enumerate(zip(replica_dirs, replicas)):
            hist = load_history(d / "locust_stats_history.csv")
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
            ax.plot(ts, rps_ts, color=colors[idx % len(colors)],
                    label=f"{n} replicas", alpha=0.8, linewidth=1.5)

        ax.set_xlabel("Time (seconds)")
        ax.set_ylabel("Requests/s")
        ax.set_title("Experiment 1: Throughput Over Time by Replica Count")
        ax.legend()
        ax.grid(True, alpha=0.3)

        fig.tight_layout()
        p = out / "exp1_throughput_timeseries.png"
        fig.savefig(p, dpi=150)
        plt.close(fig)
        print(f"Wrote {p}")

    # =========================================================================
    # Print summary
    # =========================================================================
    print("\n" + "=" * 75)
    print("  EXPERIMENT 1 SUMMARY — Scale-Out")
    print("=" * 75)
    print(f"  {'Replicas':<12} {'RPS':>10} {'Avg(ms)':>10} {'p95(ms)':>10} {'p99(ms)':>10} {'Err%':>10} {'Efficiency':>12}")
    print("-" * 75)
    for i, n in enumerate(replicas):
        eff = 100.0 * (rps_vals[i] / rps_vals[0]) / (n / replicas[0]) if rps_vals[0] > 0 else 0
        print(f"  {n:<12} {rps_vals[i]:>10.1f} {fnum(aggs[i], 'Average Response Time'):>10.0f} "
              f"{fnum(aggs[i], '95%'):>10.0f} {fnum(aggs[i], '99%'):>10.0f} "
              f"{err_rates[i]:>9.2f}% {eff:>11.1f}%")
    print("=" * 75)


if __name__ == "__main__":
    main()
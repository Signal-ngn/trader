# HTS Short Backtests — 2026-04-25

Common setup unless noted:

- Strategy: `hts` (short side via `futures-short`)
- Granularity: `FOUR_HOURS`
- Exchange: `binance`
- Date range: `2025-10-01 → 2026-04-15`
- Prod-matched flags: `--conviction-exit-threshold 0.35`, `--conviction-tighten-threshold 0.55`, `--grace-window-minutes 240`, `--grace-threshold-factor 0.5`
- Products: BTC, SOL, ENA, FLOKI, OP, POL, ATOM (all `-USD`)

## Current best per-product config

Best variant for each product across all 8 runs below. Combined portfolio avg = **+49.43%** vs baseline +17.65%.

| Product | Variant | Return | DD | Trades | Strategy params |
|---|---|---:|---:|---:|---|
| BTC | uncertainty_cap=1.7 | +25.44% | 14.1% | 26 | `--params uncertainty_cap=1.7` |
| SOL | trend-filter | +47.28% | 34.8% | 42 | `--trend-filter` |
| ENA | uncertainty_cap=1.7 | **+117.74%** | 31.4% | 59 | `--params uncertainty_cap=1.7` |
| FLOKI | trend-filter | +43.51% | 39.4% | 63 | `--trend-filter` |
| OP | uncertainty_cap=1.7 | +46.20% | 23.9% | 52 | `--params uncertainty_cap=1.7` |
| POL | baseline | +56.16% | 48.4% | 84 | (none — baseline only) |
| ATOM | uncertainty_cap=1.7 | +9.68% | 32.5% | 55 | `--params uncertainty_cap=1.7` |

All other params at their baseline values: `atr_stop_mult=3.0 confidence=0.72 exit_threshold=0.45 rr_ratio=2.0`, leverage=2, plus the prod-matched grace/conviction flags.

**Headline finding:** `uncertainty_cap=1.7` is the strongest single setting (4 of 7 wins). SOL/FLOKI prefer the trend filter; POL prefers no filter at all (its edge comes from lower-quality signals). No uniform parameter set wins everywhere — per-product config beats the best uniform by ~19pp.

## Best uniform variant ranking

| Variant | Portfolio avg | vs baseline |
|---|---:|---:|
| **`uncertainty_cap=1.7`** | **+30.69%** | **+13.0pp** ⭐ |
| `uncertainty_cap=2.5` | +24.45% | +6.8pp |
| Baseline (conf=0.72) | +17.65% | — |
| `confidence=0.80` | +16.95% | −0.7pp |
| `trend-filter=true` | +3.84% | −13.8pp |
| `uncertainty_cap=0.25` / `0.50` | 0% | (blocks all signals) |

## Run 1 — Baseline (leverage=2, atr_stop_mult=3, confidence=0.72)

| Product | Return | Win | Trades | DD | Sharpe | PF |
|---|---:|---:|---:|---:|---:|---:|
| BTC | −3.34% | 36.8% | 38 | 28.4% | −0.00 | 1.00 |
| SOL | +29.23% | 43.3% | 60 | 44.9% | 1.12 | 1.35 |
| ENA | +25.55% | 40.5% | 79 | 55.7% | 0.90 | 1.23 |
| FLOKI | −2.04% | 41.5% | 82 | 39.4% | 0.51 | 1.13 |
| OP | +10.89% | 46.8% | 79 | 27.7% | 0.63 | 1.15 |
| POL | +56.16% | 46.4% | 84 | 48.4% | 1.38 | 1.35 |
| ATOM | +7.13% | 38.3% | 81 | 42.5% | 0.56 | 1.14 |

**Portfolio avg:** +17.65% return, ~41% DD.

## Run 2 — Leverage=5, same params

| Product | Return | Win | Trades | DD | Sharpe | PF |
|---|---:|---:|---:|---:|---:|---:|
| BTC | −17.56% | 36.8% | 38 | 58.4% | 0.03 | 1.01 |
| SOL | −34.57% | 41.7% | 60 | 83.5% | 0.27 | 1.07 |
| ENA | −71.53% | 40.7% | 81 | 91.0% | 0.57 | 1.14 |
| FLOKI | −70.38% | 40.0% | 85 | 84.2% | 0.38 | 1.09 |
| OP | −69.25% | 45.0% | 80 | 82.9% | −0.00 | 1.00 |
| POL | +52.90% | 46.4% | 84 | 85.3% | 1.34 | 1.33 |
| ATOM | −22.49% | 38.3% | 81 | 76.6% | 0.73 | 1.18 |

**Portfolio avg:** −33.3% return, ~80% DD.

**Finding:** 5x is strictly worse. Trade counts are nearly identical to lev=2 (leverage doesn't change entries), so the delta is pure PnL amplification. DDs cluster at 77–91% — positions survive but bleed through the whole distance. Only POL remains profitable. No liquidations triggered anywhere (min liq distance 44–71%).

**Takeaway:** ~40% win rate × 2:1 RR × short side is too thin to survive 5x drawdown amplification on this universe. Leverage is not the knob to tune.

## Run 3 — confidence=0.80 (leverage=2, atr=3)

| Product | Return | Win | Trades | DD | Sharpe |
|---|---:|---:|---:|---:|---:|
| BTC | +5.97% | — | 30 | 25.6% | — |
| SOL | +21.99% | — | 52 | 46.6% | — |
| ENA | +72.62% | — | 66 | 39.1% | — |
| FLOKI | −18.71% | — | 62 | 51.3% | — |
| OP | +25.12% | 48.3% | 58 | 28.0% | 0.92 |
| POL | +3.55% | 43.8% | 64 | 40.3% | 0.43 |
| ATOM | +8.14% | 39.3% | 61 | 38.2% | 0.54 |

**Portfolio avg:** +16.95% return, ~38% DD.

### Delta vs baseline

| Product | ΔReturn | ΔDD | ΔTrades |
|---|---:|---:|---:|
| BTC | +9.3 pp | −2.8 pp | −8 |
| SOL | −7.2 pp | +1.7 pp | −8 |
| ENA | +47.1 pp | **−16.6 pp** | −13 |
| FLOKI | −16.7 pp | +11.9 pp | −20 |
| OP | +14.2 pp | +0.3 pp | −21 |
| POL | **−52.6 pp** | −8.1 pp | −20 |
| ATOM | +1.0 pp | −4.3 pp | −20 |

**Finding:** portfolio return is roughly flat (+17.7% → +17.0%) but per-product variance widens. 4 products improve (BTC / ENA / OP / ATOM); 3 get worse (SOL / FLOKI / POL). POL's edge came specifically from lower-confidence signals — at 0.80 it collapses from +56% to +4%. ENA is the opposite: tighter entry filter nearly triples its return and slashes DD by ~17 pp.

**Takeaway:** optimal confidence threshold is not uniform across products.

## Run 4 — `uncertainty_cap=0.25`

**All 7 products: 0 trades.** Cap is well below the actual `Uncertainty` distribution — every entry blocked.

## Run 5 — `trend-filter=true`

| Product | Return | Win | Trades | DD | Sharpe |
|---|---:|---:|---:|---:|---:|
| BTC | −0.86% | — | 30 | 22.5% | — |
| SOL | +47.28% | — | 42 | 34.8% | — |
| ENA | −31.23% | — | 67 | 54.5% | — |
| FLOKI | +43.51% | — | 63 | 39.4% | — |
| OP | +7.53% | — | 62 | 28.5% | — |
| POL | −2.24% | — | 61 | 52.3% | — |
| ATOM | −37.08% | — | 57 | 54.9% | — |

**Portfolio avg:** +3.84% (down from +17.65% baseline).

### Delta vs baseline

| Product | ΔReturn | ΔDD | ΔTrades |
|---|---:|---:|---:|
| BTC | +2.5 pp | −5.9 pp | −8 |
| SOL | **+18.0 pp** | −10.1 pp | −18 |
| ENA | **−56.8 pp** | −1.2 pp | −12 |
| FLOKI | **+45.5 pp** | 0.0 pp | −19 |
| OP | −3.4 pp | +0.8 pp | −17 |
| POL | **−58.4 pp** | +3.9 pp | −23 |
| ATOM | **−44.2 pp** | +12.4 pp | −24 |

**Finding:** worst portfolio result so far. Massive bimodal effect — SOL/FLOKI improve dramatically, while ENA/POL/ATOM collapse by 40–60 pp each. The trend filter helps when the market trends genuinely against the short side, but hurts on products where the strategy already had a viable countertrend edge.

## Run 6 — `uncertainty_cap=0.50`

**All 7 products: 0 trades again.** TFT `Uncertainty` field is consistently above 0.50 on this universe.

`spot-canvas-app/internal/strategy/hts/rl.go:220-229` compares `f.Uncertainty` (not the quantile spread) directly against the cap. At 0.50 every signal still trips it.

### Empirical Uncertainty distribution (from prod `signals` table, `hts_short`, last 30 days, n=80)

| pct | min | p10 | p25 | **p50** | p75 | p90 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| value | 0.87 | 1.24 | 1.45 | **1.66** | 2.11 | 2.58 | 50.70 |

Confirms 0.25 / 0.50 are well below the minimum — they correctly block 100% of signals. Meaningful caps for this strategy are in the **1.5–2.5** range.

## Run 7 — `uncertainty_cap=1.7`

| Product | Return | Win | Trades | DD | Sharpe |
|---|---:|---:|---:|---:|---:|
| BTC | +25.44% | — | 26 | **14.1%** | — |
| SOL | +24.60% | — | 46 | 43.4% | — |
| ENA | **+117.74%** | — | 59 | 31.4% | — |
| FLOKI | +7.67% | — | 55 | 47.5% | — |
| OP | +46.20% | — | 52 | 23.9% | — |
| POL | −16.53% | — | 58 | 51.7% | — |
| ATOM | +9.68% | — | 55 | 32.5% | — |

**Portfolio avg:** **+30.69%** (best uniform variant — +13.0pp over baseline). DDs improve on most products (BTC 28→14%, ENA 56→31%, OP 28→24%, ATOM 43→33%).

POL collapses again, consistent with conf=0.80 — POL's edge comes specifically from lower-quality signals.

## Run 8 — `uncertainty_cap=2.5`

| Product | Return | Win | Trades | DD | Sharpe |
|---|---:|---:|---:|---:|---:|
| BTC | −2.67% | 36.8% | 38 | 28.4% | 0.03 |
| SOL | +29.41% | 44.1% | 59 | 44.9% | 1.13 |
| ENA | **+86.24%** | 44.0% | 75 | 43.6% | 1.56 |
| FLOKI | +7.66% | 44.2% | 77 | 36.4% | 0.67 |
| OP | +1.26% | 45.3% | 75 | 29.5% | 0.43 |
| POL | +41.57% | 44.6% | 83 | 49.3% | 1.16 |
| ATOM | +7.68% | 37.7% | 77 | 42.9% | 0.57 |

**Portfolio avg:** **+24.45%** (+6.8pp over baseline). Milder than UNC1.7 — filters fewer signals so most products stay close to baseline, but still net positive across the board (no big collapses).

## Open questions / next sweeps

- **`uncertainty_cap=1.5`** — even more aggressive. p25 of distribution is 1.45, so this would let only the top quartile of signals through. Could push BTC/ENA further or finally tip into over-filtering.
- **UNC1.7 + conf=0.80 stacked** *(in flight)* — combine the two best non-baseline filters. Untested whether they compound or interfere.
- **`median_return_floor=0.005`** *(in flight)* — blocks low-expected-move entries. Complementary to uncertainty cap.
- **UNC1.7 + atr_stop_mult=2.0** *(in flight)* — tighter stops on the new strongest setting; could compound the DD reduction.
- **Per-product config** — formalise the per-product winners table into trader runtime config (UNC1.7 default, trend-filter for SOL/FLOKI, baseline for POL).

## CLI template used

```bash
trader backtest run \
  --exchange binance --product $PRODUCT --strategy hts \
  --granularity FOUR_HOURS --mode futures-short --leverage 2 \
  --start 2025-10-01 --end 2026-04-15 \
  --params atr_stop_mult=3.0 --params confidence=0.72 \
  --params exit_threshold=0.45 --params rr_ratio=2.0 \
  --conviction-exit-threshold 0.35 --conviction-tighten-threshold 0.55 \
  --grace-window-minutes 240 --grace-threshold-factor 0.5
```

## Known display bug

`trader backtest job` renders `Leverage: -` regardless of what leverage was used. The server's `BacktestResultRecord` (`spot-canvas-app/internal/shared/storage/backtest_result.go:58-78`) has no top-level `Leverage` field — it's only persisted per trade and as `futures_metrics.avg_leverage`. The CLI struct (`cmd/trader/cmd_backtest.go:53`) expects a top-level field that the server never writes, so it decodes to 0 and `fmtLeverage(0)` prints `-`. Leverage is still applied correctly in the simulation (confirmed by the matched trade counts between lev=2 and lev=5 runs); only the summary display is missing the value.

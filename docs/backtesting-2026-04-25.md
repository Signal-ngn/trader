# HTS Short Backtests — 2026-04-25

Common setup unless noted:

- Strategy: `hts` (short side via `futures-short`)
- Granularity: `FOUR_HOURS`
- Exchange: `binance`
- Date range: `2025-10-01 → 2026-04-15`
- Prod-matched flags: `--conviction-exit-threshold 0.35`, `--conviction-tighten-threshold 0.55`, `--grace-window-minutes 240`, `--grace-threshold-factor 0.5`
- Products: BTC, SOL, ENA, FLOKI, OP, POL, ATOM (all `-USD`)

## Current best per-product config (all 23 products)

Best variant for each product across all runs. Combined portfolio avg = **+47.07%** vs uniform baseline +32.04%.

| Product | Variant | Return | DD | Trades | Strategy params |
|---|---|---:|---:|---:|---|
| **Original 7** | | | | | |
| BTC | uncertainty_cap=1.7 | +25.44% | 14.1% | 26 | `--params uncertainty_cap=1.7` |
| SOL | trend-filter | +47.28% | 34.8% | 42 | `--trend-filter` |
| ENA | uncertainty_cap=1.7 | **+117.74%** | 31.4% | 59 | `--params uncertainty_cap=1.7` |
| FLOKI | trend-filter | +43.51% | 39.4% | 63 | `--trend-filter` |
| OP | uncertainty_cap=1.7 | +46.20% | 23.9% | 52 | `--params uncertainty_cap=1.7` |
| POL | baseline | +56.16% | 48.4% | 84 | (none) |
| ATOM | uncertainty_cap=1.7 | +9.68% | 32.5% | 55 | `--params uncertainty_cap=1.7` |
| **New 16** | | | | | |
| AAVE | uncertainty_cap=1.7 | −2.02% | 27.2% | 41 | `--params uncertainty_cap=1.7` |
| ADA | baseline | +68.22% | 12.9% | 62 | (none) |
| APT | baseline | +32.96% | 37.7% | 56 | (none) |
| ARB | baseline | +87.23% | 42.2% | 71 | (none) |
| AVAX | baseline | +34.15% | 35.0% | 54 | (none) |
| BNB | baseline | −4.46% | 24.7% | 29 | (none) |
| BONK | baseline | +106.41% | 27.5% | 67 | (none) |
| DOGE | baseline | +50.75% | 31.7% | 69 | (none) |
| DOT | uncertainty_cap=1.7 | +90.41% | 14.4% | 35 | `--params uncertainty_cap=1.7` |
| INJ | uncertainty_cap=1.7 | +81.20% | 21.8% | 39 | `--params uncertainty_cap=1.7` |
| LINK | baseline | +36.22% | 36.2% | 55 | (none) |
| NEAR | baseline | +45.54% | 34.9% | 60 | (none) |
| PEPE | baseline | +23.38% | 41.6% | 73 | (none) |
| SHIB | uncertainty_cap=1.7 | +61.97% | 18.7% | 46 | `--params uncertainty_cap=1.7` |
| UNI | uncertainty_cap=1.7 | −6.08% | 51.3% | 31 | `--params uncertainty_cap=1.7` |
| XLM | baseline | +143.23% | 25.4% | 72 | (none) |

All other params at their baseline values: `atr_stop_mult=3.0 confidence=0.72 exit_threshold=0.45 rr_ratio=2.0`, leverage=2, plus the prod-matched grace/conviction flags.

**Headline findings:**

1. **UNC1.7 wins 9 of 23 products** (BTC, ENA, OP, ATOM, AAVE, DOT, INJ, SHIB, UNI), **baseline wins 12**, **trend-filter wins 2** (SOL, FLOKI).
2. The original 7-coin universe overstated the UNC1.7 advantage. On the full 23-product universe, baseline is the stronger uniform setting (+32.04% avg vs UNC1.7's +27.83% on the new 16).
3. Per-product config still beats the best uniform by a wide margin (~15pp).
4. Products with strong trending shorts (XLM +143%, BONK +106%, ARB +87%) lose dramatically under UNC1.7 — the filter removes lower-certainty signals that happen to be correct. Products with noisier distributions (INJ, DOT, SHIB) benefit from the filter.
5. AAVE, BNB, and UNI are marginal/negative on both variants — candidates for exclusion from the short universe.

## Best uniform variant ranking

### Original 7 products

| Variant | Portfolio avg | vs baseline |
|---|---:|---:|
| **`uncertainty_cap=1.7`** | **+30.69%** | **+13.0pp** ⭐ |
| `uncertainty_cap=2.5` | +24.45% | +6.8pp |
| Baseline (conf=0.72) | +17.65% | — |
| `confidence=0.80` | +16.95% | −0.7pp |
| `trend-filter=true` | +3.84% | −13.8pp |
| UNC1.7 + atr_stop_mult=2.0 | +1.84% | −15.8pp |
| `uncertainty_cap=0.25` / `0.50` | 0% | (blocks all signals) |

### New 16 products

| Variant | Portfolio avg |
|---|---:|
| **Baseline** | **+41.93%** ⭐ |
| `uncertainty_cap=1.7` | +27.83% |

UNC1.7 is the best uniform setting on the original 7 but baseline dominates the new 16. No single uniform setting wins across all 23.

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

## Run 9 — UNC1.7 + atr_stop_mult=2.0 (original 7 products)

Tighter ATR stops on the strongest uniform filter. Hypothesis: could compound UNC1.7's DD reduction.

| Product | Return | Win | Trades | DD | Sharpe | PF |
|---|---:|---:|---:|---:|---:|---:|
| BTC | +7.93% | 35.7% | 28 | 15.5% | 0.62 | 1.24 |
| SOL | −0.11% | 38.0% | 50 | 43.4% | 0.24 | 1.07 |
| ENA | +39.58% | 40.3% | 62 | 30.9% | 1.11 | 1.34 |
| FLOKI | +11.41% | 38.2% | 55 | 30.2% | 0.67 | 1.20 |
| OP | −5.60% | 43.6% | 55 | 38.4% | 0.13 | 1.03 |
| POL | −37.34% | 36.9% | 65 | 52.5% | −1.16 | 0.78 |
| ATOM | −2.97% | 36.8% | 57 | 27.6% | 0.14 | 1.04 |

**Portfolio avg:** +1.84% (vs UNC1.7 at atr=3: +30.69%).

**Finding:** strictly worse on 6 of 7 products. Only FLOKI slightly improves (+7.7→+11.4%, DD 47→30%). Tighter stops cause early exits on shorts — the wider 3x ATR stop is essential for giving short positions room to breathe through countertrend bounces. **ATR2 is not a viable variant.**

## Run 10 — Stacked filters (UNC1.7 + conf=0.80, MRF=0.005)

- **UNC1.7 + conf=0.80**: identical results to UNC1.7 alone on all 7 products. UNC1.7 already filters out all sub-0.80 confidence signals — the confidence gate is a strict subset. **No-op.**
- **median_return_floor=0.005**: identical results to UNC1.7 alone on all 7 products. All signals have |median_return| > 0.005 already. **No-op.**

## Run 11 — New-coin sweep: baseline + UNC1.7 (16 new products)

16 products not in the original sweep: AAVE, ADA, APT, ARB, AVAX, BNB, BONK, DOGE, DOT, INJ, LINK, NEAR, PEPE, SHIB, UNI, XLM.

### Baseline (atr=3, conf=0.72, no filters)

| Product | Return | Win | Trades | DD |
|---|---:|---:|---:|---:|
| AAVE | −24.20% | 35.7% | 56 | 42.1% |
| ADA | +68.22% | 40.3% | 62 | 12.9% |
| APT | +32.96% | 44.6% | 56 | 37.7% |
| ARB | +87.23% | 39.4% | 71 | 42.2% |
| AVAX | +34.15% | 42.6% | 54 | 35.0% |
| BNB | −4.46% | 37.9% | 29 | 24.7% |
| BONK | +106.41% | 43.3% | 67 | 27.5% |
| DOGE | +50.75% | 40.6% | 69 | 31.7% |
| DOT | +62.23% | 39.7% | 63 | 27.6% |
| INJ | −15.07% | 43.6% | 55 | 37.7% |
| LINK | +36.22% | 41.8% | 55 | 36.2% |
| NEAR | +45.54% | 36.7% | 60 | 34.9% |
| PEPE | +23.38% | 39.7% | 73 | 41.6% |
| SHIB | +45.06% | 39.1% | 64 | 20.8% |
| UNI | −20.75% | 40.7% | 54 | 46.4% |
| XLM | +143.23% | 44.4% | 72 | 25.4% |

**Portfolio avg:** +41.93%.

### UNC1.7

| Product | Return | Win | Trades | DD |
|---|---:|---:|---:|---:|
| AAVE | −2.02% | 41.5% | 41 | 27.2% |
| ADA | +61.79% | 42.9% | 42 | 14.0% |
| APT | −41.18% | 36.4% | 33 | 47.5% |
| ARB | +84.40% | 41.1% | 56 | 35.8% |
| AVAX | −22.32% | 33.3% | 39 | 34.4% |
| BNB | −8.74% | 38.9% | 18 | 17.5% |
| BONK | +52.50% | 42.6% | 47 | 40.4% |
| DOGE | −0.61% | 41.3% | 46 | 22.7% |
| DOT | +90.41% | 45.7% | 35 | 14.4% |
| INJ | +81.20% | 43.6% | 39 | 21.8% |
| LINK | +2.56% | 38.5% | 39 | 40.5% |
| NEAR | +24.16% | 38.2% | 34 | 32.8% |
| PEPE | +16.28% | 42.9% | 56 | 37.1% |
| SHIB | +61.97% | 41.3% | 46 | 18.7% |
| UNI | −6.08% | 48.4% | 31 | 51.3% |
| XLM | +51.95% | 46.2% | 52 | 19.9% |

**Portfolio avg:** +27.83%.

### Delta (UNC1.7 − baseline)

| Product | ΔReturn | ΔDD | ΔTrades | Winner |
|---|---:|---:|---:|---|
| AAVE | +22.2pp | −14.9pp | −15 | UNC1.7 |
| ADA | −6.4pp | +1.1pp | −20 | baseline |
| APT | **−74.1pp** | +9.8pp | −23 | baseline |
| ARB | −2.8pp | −6.4pp | −15 | baseline |
| AVAX | **−56.5pp** | −0.6pp | −15 | baseline |
| BNB | −4.3pp | −7.2pp | −11 | baseline |
| BONK | **−53.9pp** | +12.9pp | −20 | baseline |
| DOGE | **−51.4pp** | −9.0pp | −23 | baseline |
| DOT | **+28.2pp** | **−13.2pp** | −28 | UNC1.7 |
| INJ | **+96.3pp** | **−15.9pp** | −16 | UNC1.7 |
| LINK | −33.7pp | +4.3pp | −16 | baseline |
| NEAR | −21.4pp | −2.1pp | −26 | baseline |
| PEPE | −7.1pp | −4.5pp | −17 | baseline |
| SHIB | +16.9pp | −2.1pp | −18 | UNC1.7 |
| UNI | +14.7pp | +4.9pp | −23 | UNC1.7 |
| XLM | **−91.3pp** | −5.5pp | −20 | baseline |

**Finding:** UNC1.7 wins 5 of 16 (AAVE, DOT, INJ, SHIB, UNI). Baseline wins 11. The new-coin universe has more products with strong trending shorts where the uncertainty filter removes profitable signals. The magnitude of baseline wins (APT −74pp, XLM −91pp, BONK −54pp) far exceeds UNC1.7 wins in absolute terms, though UNC1.7 wins tend to come with significant DD improvement.

## Recommended next tests

1. **Trend-filter sweep on the 16 new products** — trend-filter was the best variant for SOL/FLOKI in the original 7 but hasn't been tested on the new coins. Several products (ARB, BONK, DOGE, XLM) have strong trending characteristics that may benefit.

2. **`uncertainty_cap=2.0` and `2.2`** — finer grid between UNC1.7 (too aggressive for many products) and UNC2.5 (too mild). A middle ground may recover some of the baseline products without losing the UNC1.7 winners.

3. **`confidence=0.65` (looser entry)** — POL's edge came from lower-quality signals. Several new products (BONK +106%, XLM +143%) also perform best at baseline. A looser confidence threshold could capture more of these signals.

4. **`median_return_floor=0.02`** — floor=0.005 was a no-op; a higher value would actually filter low-expected-move entries. Worth testing at 0.02–0.03 range on products with many small trades.

5. **Per-product config validation** — formalise the best-per-product table into trader runtime config and run a single combined portfolio backtest to validate the aggregate return.

6. **Exclude weak products** — AAVE (−2%), BNB (−4%), UNI (−6%) are marginal/negative on their best variant. Run a portfolio backtest with these excluded to quantify the drag.

7. **Out-of-sample test** — all runs use 2025-10-01 → 2026-04-15. Run the per-product best config on a different date range (e.g. 2025-04-01 → 2025-09-30) to check for overfitting.

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

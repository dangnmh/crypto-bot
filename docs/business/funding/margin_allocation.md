# Candidate Margin Allocation Specification

## Overview
This document specifies the strategy and algorithm for allocating margin and computing order trade volume across scanned trading candidates in the funding bot.

The allocation logic prioritizes opening **more orders** across qualified candidates while strictly respecting account margin limits, candidate margin caps, market liquidity/impact ceilings, and exchange tier-based leverage limits.

---

## Architecture & Design (Domain-Driven Design)

The allocation logic is modeled as a **Domain Strategy Component** inside `internal/bots/funding/domain/allocator.go`:

```
+-------------------------------------------------------------+
|                     ScheduleScannerJob                      |
|                  (Application Component)                    |
+------------------------------+------------------------------+
                               |
                               v
+-------------------------------------------------------------+
|                       MarginAllocator                       |
|                       (Domain Interface)                    |
+------------------------------+------------------------------+
                               |
                               v
+-------------------------------------------------------------+
|             AscendingVolumeMarginAllocator                  |
|                   (Domain Strategy)                         |
+-------------------------------------------------------------+
```

---

## Allocation Algorithm (`AscendingVolumeMarginAllocator`)

### Input Parameters
1. `candidates`: List of scanned opportunity candidates (each carrying `Candidate.Config.Leverage`).
2. `totalMarginUSD`: Total account balance / margin pool available for trading.
3. `maxCandidateMargin`: Configured maximum USDT margin allowed per candidate (0 = unconstrained).
4. `maxImpactRatio`: Percentage of 1-minute market volume allowed per trade (e.g. 5.0 = 5%).

### Algorithm Steps

1. **Candidate Sorting**:
   Sort candidates in **ascending order** of 24h volume (`vol24hUSDT`):
   $$\text{candidates.sort}(vol24hUSDT \text{ asc})$$
   *Rationale*: Processing lower-volume candidates first uses smaller margin amounts, preserving total margin to execute a higher number of orders across candidates.

2. **Sequential Iteration**:
   For each candidate in the sorted list, as long as $\text{totalMarginUSD} > 0$:

   a. **Candidate Margin Cap**:
      $$\text{candidateMarginLimit} = \min(\text{totalMarginUSD}, \text{maxCandidateMargin})$$

   b. **Market Liquidity Ceiling**:
      $$\text{volMinuteUSDT} = \frac{\text{vol24hUSDT}}{1440}$$
      $$\text{maxTradeVolUSDT} = \left(\frac{\text{maxImpactRatio}}{100}\right) \times \text{volMinuteUSDT}$$

   c. **Target vs. Actual Position Trade Volume**:
      $$\text{targetTradeVolUSDT} = \text{candidateMarginLimit} \times \text{Candidate.Config.Leverage}$$
      $$\text{actualTradeVolUSDT} = \min(\text{targetTradeVolUSDT}, \text{maxTradeVolUSDT})$$

   d. **Exchange Risk-Limit Leverage Resolution**:
      Resolve maximum allowed leverage from the exchange for position size $\text{actualTradeVolUSDT}$:
      $$\text{actualLeverage} = \min(\text{Candidate.Config.Leverage}, \text{GetMaxLeverageForValue}(\text{symbol}, \text{actualTradeVolUSDT}))$$

   e. **Required Margin Calculation**:
      $$\text{needMarginUSDT} = \frac{\text{actualTradeVolUSDT}}{\text{actualLeverage}}$$

   f. **Margin Cap Synchronization**:
      If $\text{needMarginUSDT} > \text{candidateMarginLimit}$:
      $$\text{needMarginUSDT} = \text{candidateMarginLimit}$$
      $$\text{actualTradeVolUSDT} = \text{needMarginUSDT} \times \text{actualLeverage}$$

   g. **Deduction & Candidate Configuration Update**:
      $$\text{totalMarginUSD} \leftarrow \text{totalMarginUSD} - \text{needMarginUSDT}$$
      Set candidate configuration:
      $$\text{Candidate.Config.MarginUSDT} = \text{needMarginUSDT}$$
      $$\text{Candidate.Config.Leverage} = \text{actualLeverage}$$
      $$\text{Candidate.Volume} = \text{CalculateVolume}()$$

---

## Edge Case Handling

1. **Zero / Negative Remaining Margin**: Iteration stops immediately when $\text{totalMarginUSD} \le 0$.
2. **Missing Market Ticker / Volume Data**: If `vol24hUSDT` is 0 or missing, `maxTradeVolUSDT` evaluates to 0, resulting in 0 allocated volume.
3. **Exchange Leverage Risk Tiers**: If an exchange enforces lower leverage for large position sizes, `actualLeverage` drops appropriately without exceeding the candidate's margin budget.

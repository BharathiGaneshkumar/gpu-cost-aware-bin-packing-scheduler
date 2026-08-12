#!/bin/bash
# Runs the tiebreak-scoring ablation: fixed-size jobs designed to force
# genuine scoring ties, isolating the tie-breaker cascade (duration-fit,
# cost-tier, LRU) that Phase 4's random workload rarely exercised.
# Results land in results/tiebreak-scoring/, kept separate from the
# baseline-comparison (Phase 4) dataset.

set -e

OUT_DIR="results/tiebreak-scoring"
SEED_COUNT=5
TIEBREAK_COUNT=14
TIEBREAK_UNITS=3
TIEBREAK_MEMGB=5

mkdir -p "$OUT_DIR"

echo "=== LeastAllocated ==="
go run cmd/benchmark/main.go \
  --context kind-gpu-scheduler-leastallocated \
  --mode shared \
  --strategy LeastAllocated \
  --jobMode tiebreak \
  --loadLevels 70 \
  --seedCount $SEED_COUNT \
  --tiebreakCount $TIEBREAK_COUNT \
  --tiebreakUnits $TIEBREAK_UNITS \
  --tiebreakMemGB $TIEBREAK_MEMGB \
  --outDir "$OUT_DIR"

echo "=== MostAllocated ==="
go run cmd/benchmark/main.go \
  --context kind-gpu-scheduler-mostallocated \
  --mode shared \
  --strategy MostAllocated \
  --jobMode tiebreak \
  --loadLevels 70 \
  --seedCount $SEED_COUNT \
  --tiebreakCount $TIEBREAK_COUNT \
  --tiebreakUnits $TIEBREAK_UNITS \
  --tiebreakMemGB $TIEBREAK_MEMGB \
  --outDir "$OUT_DIR"

echo "=== OurWebhook ==="
go run cmd/benchmark/main.go \
  --context kind-gpu-scheduler \
  --mode webhook \
  --strategy OurWebhook \
  --jobMode tiebreak \
  --loadLevels 70 \
  --seedCount $SEED_COUNT \
  --tiebreakCount $TIEBREAK_COUNT \
  --tiebreakUnits $TIEBREAK_UNITS \
  --tiebreakMemGB $TIEBREAK_MEMGB \
  --outDir "$OUT_DIR"

echo "All tiebreak runs complete."
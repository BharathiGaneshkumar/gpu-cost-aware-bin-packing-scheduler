#!/bin/bash

LOAD_LEVELS="50,75,90,110"
SEED_COUNT=10
OUT_DIR="results"

echo "=== Running LeastAllocated (kind-gpu-scheduler-leastallocated) ==="
kubectl --context kind-gpu-scheduler-leastallocated delete pods --all -n default --grace-period=0 --force
go run ./cmd/benchmark \
  --context kind-gpu-scheduler-leastallocated \
  --mode shared \
  --strategy LeastAllocated \
  --loadLevels "$LOAD_LEVELS" \
  --seedCount "$SEED_COUNT" \
  --outDir "$OUT_DIR"

echo "=== Running MostAllocated (kind-gpu-scheduler-mostallocated) ==="
kubectl --context kind-gpu-scheduler-mostallocated delete pods --all -n default --grace-period=0 --force
go run ./cmd/benchmark \
  --context kind-gpu-scheduler-mostallocated \
  --mode shared \
  --strategy MostAllocated \
  --loadLevels "$LOAD_LEVELS" \
  --seedCount "$SEED_COUNT" \
  --outDir "$OUT_DIR"

echo "=== Running OurWebhook (kind-gpu-scheduler) ==="
kubectl --context kind-gpu-scheduler delete pods --all -n default --grace-period=0 --force
go run ./cmd/benchmark \
  --context kind-gpu-scheduler \
  --mode webhook \
  --strategy OurWebhook \
  --loadLevels "$LOAD_LEVELS" \
  --seedCount "$SEED_COUNT" \
  --outDir "$OUT_DIR"

echo ""
echo "=== All strategies complete. Results in $OUT_DIR/ ==="
ls "$OUT_DIR" | wc -l
echo "JSON result files written."
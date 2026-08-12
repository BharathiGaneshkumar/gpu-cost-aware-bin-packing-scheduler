# Benchmark Results

Aggregated from 15 individual runs.

| Load | Strategy | Runs | Avg Scheduled Units % | Avg Denied/Pending % | Avg Max Free/GPU | Avg Utilization % | Avg Idle $ Wasted | Avg Cost/Job | Probe Success Rate |
|---|---|---|---|---|---|---|---|---|---|
| 70% | LeastAllocated | 5 | 100.0% | 0.0% | 7.0/10 (total free 18.0/60) | 70.0% | $3.12 | $0.591 | 0.0% |
| 70% | MostAllocated | 5 | 100.0% | 0.0% | 10.0/10 (total free 18.0/60) | 70.0% | $3.12 | $0.411 | 100.0% |
| 70% | OurWebhook | 5 | 100.0% | 0.0% | 10.0/10 (total free 18.0/60) | 70.0% | $3.12 | $0.411 | 100.0% |

# Benchmark Results

Aggregated from 120 individual runs across 4 load levels (50%, 75%, 90%, 110% of total compute capacity), 10 seeds per level per strategy, 3 strategies.

| Load | Strategy | Runs | Avg Scheduled Units % | Avg Denied/Pending % | Avg Max Free/GPU | Avg Utilization % | Avg Idle $ Wasted | Avg Cost/Job | Probe Success Rate |
|---|---|---|---|---|---|---|---|---|---|
| 50% | LeastAllocated | 10 | 97.8% | 1.2% | 8.9/10 (total free 29.2/60) | 51.3% | $5.06 | $0.753 | 30.0% |
| 50% | MostAllocated | 10 | 100.0% | 0.0% | 9.8/10 (total free 28.4/60) | 52.7% | $4.92 | $0.600 | 90.0% |
| 50% | OurWebhook | 10 | 100.0% | 0.0% | 9.8/10 (total free 28.4/60) | 52.7% | $4.92 | $0.600 | 90.0% |
| 75% | LeastAllocated | 10 | 84.1% | 9.6% | 7.8/10 (total free 21.1/60) | 64.8% | $3.66 | $0.671 | 20.0% |
| 75% | MostAllocated | 10 | 93.6% | 4.3% | 6.7/10 (total free 16.7/60) | 72.2% | $2.89 | $0.653 | 20.0% |
| 75% | OurWebhook | 10 | 93.4% | 5.0% | 6.7/10 (total free 16.8/60) | 72.0% | $2.91 | $0.654 | 20.0% |
| 90% | LeastAllocated | 10 | 74.1% | 15.9% | 6.9/10 (total free 18.1/60) | 69.8% | $3.14 | $0.617 | 0.0% |
| 90% | MostAllocated | 10 | 82.1% | 11.9% | 6.5/10 (total free 13.6/60) | 77.3% | $2.36 | $0.632 | 20.0% |
| 90% | OurWebhook | 10 | 83.2% | 11.2% | 6.3/10 (total free 13.0/60) | 78.3% | $2.25 | $0.635 | 20.0% |
| 110% | LeastAllocated | 10 | 63.5% | 24.4% | 6.6/10 (total free 16.6/60) | 72.3% | $2.88 | $0.611 | 0.0% |
| 110% | MostAllocated | 10 | 70.5% | 21.0% | 6.0/10 (total free 11.9/60) | 80.2% | $2.06 | $0.631 | 10.0% |
| 110% | OurWebhook | 10 | 70.5% | 21.0% | 6.0/10 (total free 11.9/60) | 80.2% | $2.06 | $0.631 | 10.0% |

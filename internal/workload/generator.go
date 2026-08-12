package workload

import (
	"fmt"
	"math/rand"
	"strconv"
)

// Job represents one simulated GPU job in a test batch.
type Job struct {
	Name                    string
	Units                   int
	MemoryGB                int
	Type                    string // "latency-sensitive" or "batch-tolerant"
	ExpectedDurationSeconds int
}

type sizeClass struct {
	min, max int
	weight   float64
}

// distribution mirrors a plausible real GPU workload mix: mostly small
// jobs, some medium, few large -- not tuned to flatter any one strategy.
var distribution = []sizeClass{
	{min: 1, max: 3, weight: 0.6}, // small
	{min: 4, max: 6, weight: 0.3}, // medium
	{min: 7, max: 9, weight: 0.1}, // large
}

const (
	// batchTolerantProbability mirrors real-world practice cited in
	// research: teams commonly run 40-60% of inference volume on
	// spot/discount capacity -- 60% batch-tolerant here.
	batchTolerantProbability = 0.6

	shortJobProbability = 0.5
	shortJobMinSeconds  = 30
	shortJobMaxSeconds  = 300
	longJobMinSeconds   = 301
	longJobMaxSeconds   = 3600
)

// GenerateBatch produces a reproducible batch of n jobs using the given
// seed. Same seed + same n always produces the identical batch.
func GenerateBatch(n int, seed int64) []Job {
	rng := rand.New(rand.NewSource(seed))
	jobs := make([]Job, n)
	for i := 0; i < n; i++ {
		class := pickClass(rng)
		units := class.min + rng.Intn(class.max-class.min+1)

		jobs[i] = Job{
			Name:                    "workload-job-" + strconv.Itoa(i),
			Units:                   units,
			MemoryGB:                generateMemoryGB(rng, units),
			Type:                    generateJobType(rng),
			ExpectedDurationSeconds: generateDuration(rng),
		}
	}
	return jobs
}

// GenerateBatchForDemand generates jobs one at a time (using the same
// seeded distribution as GenerateBatch) until cumulative Units reaches
// targetDemand, then stops -- unlike GenerateBatch, which fixes job
// COUNT upfront and only approximates total demand via an average job
// size estimate. This directly controls the thing that actually matters
// for load-level testing (total demand vs. capacity), rather than
// hoping job-count-based sizing lands close by chance.
func GenerateBatchForDemand(targetDemand int, seed int64) []Job {
	rng := rand.New(rand.NewSource(seed))
	var jobs []Job
	cumulative := 0
	i := 0
	for cumulative < targetDemand {
		class := pickClass(rng)
		units := class.min + rng.Intn(class.max-class.min+1)

		jobs = append(jobs, Job{
			Name:                    "workload-job-" + strconv.Itoa(i),
			Units:                   units,
			MemoryGB:                generateMemoryGB(rng, units),
			Type:                    generateJobType(rng),
			ExpectedDurationSeconds: generateDuration(rng),
		})
		cumulative += units
		i++
	}
	return jobs
}

func pickClass(rng *rand.Rand) sizeClass {
	r := rng.Float64()
	cumulative := 0.0
	for _, c := range distribution {
		cumulative += c.weight
		if r < cumulative {
			return c
		}
	}
	return distribution[len(distribution)-1]
}

// generateMemoryGB loosely correlates memory need with compute units
// (roughly 2-4GB per unit) plus independent randomness.
func generateMemoryGB(rng *rand.Rand, units int) int {
	perUnitGB := 2 + rng.Intn(3)
	base := units * perUnitGB
	jitter := rng.Intn(5) - 2
	mem := base + jitter
	if mem < 1 {
		mem = 1
	}
	return mem
}

func generateJobType(rng *rand.Rand) string {
	if rng.Float64() < batchTolerantProbability {
		return "batch-tolerant"
	}
	return "latency-sensitive"
}

func generateDuration(rng *rand.Rand) int {
	if rng.Float64() < shortJobProbability {
		return shortJobMinSeconds + rng.Intn(shortJobMaxSeconds-shortJobMinSeconds+1)
	}
	return longJobMinSeconds + rng.Intn(longJobMaxSeconds-longJobMinSeconds+1)
}

// GenerateTiebreakBatch produces jobs of identical size (fixed units and
// memory) so that best-fit scoring alone cannot distinguish between GPUs
// with equal leftover capacity. This isolates the tie-breaker cascade
// (duration-fit, cost-tier, LRU) for evaluation, which Phase 4's random
// workload rarely exercised since job sizes rarely tied exactly.
func GenerateTiebreakBatch(count int, fixedUnits, fixedMemoryGB int) []Job {
	jobs := make([]Job, 0, count)
	for i := 0; i < count; i++ {
		jobType := "batch-tolerant"
		if i%2 == 0 {
			jobType = "latency-sensitive"
		}
		duration := 60 // short
		if i%2 == 1 {
			duration = 900 // long
		}
		jobs = append(jobs, Job{
			Name:                    fmt.Sprintf("tiebreak-job-%d", i),
			Units:                   fixedUnits,
			MemoryGB:                fixedMemoryGB,
			Type:                    jobType,
			ExpectedDurationSeconds: duration,
		})
	}
	return jobs
}

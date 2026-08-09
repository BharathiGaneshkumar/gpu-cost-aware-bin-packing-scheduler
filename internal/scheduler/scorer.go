package scheduler

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

type Tier string

const (
	TierPremium Tier = "premium"
	TierMid     Tier = "mid"
	TierEconomy Tier = "economy"
)

type JobType string

const (
	JobLatencySensitive JobType = "latency-sensitive"
	JobBatchTolerant    JobType = "batch-tolerant"
)

// GPU represents one simulated GPU's current allocation state.
type GPU struct {
	ID              string
	ComputeCapacity int
	FreeCompute     int
	MemoryGB        int
	FreeMemoryGB    int
	Tier            Tier
	CostPerUnitHour float64
	LastUsed        time.Time
}

// Job represents an incoming placement request.
type Job struct {
	Units                   int
	MemoryGB                int
	Type                    JobType
	ExpectedDurationSeconds int
}

var (
	ErrNoFit  = errors.New("no GPU has sufficient free compute and memory capacity")
	ErrNoGPUs = errors.New("no GPUs provided to select from")
	ErrNoTier = errors.New("no GPU tier is eligible for this job type")
)

// eligibleTiers returns which GPU tiers a job of the given type is allowed
// to land on. Latency-sensitive jobs are hard-excluded from economy tier
// -- this is a correctness rule, not just a scoring preference, since a
// latency-sensitive job silently landing on slow hardware would defeat
// the point of tagging it.
func eligibleTiers(jobType JobType) map[Tier]bool {
	switch jobType {
	case JobLatencySensitive:
		return map[Tier]bool{TierPremium: true, TierMid: true}
	default: // batch-tolerant, or unspecified defaults to fully open
		return map[Tier]bool{TierPremium: true, TierMid: true, TierEconomy: true}
	}
}

// SelectGPU picks the best GPU for the given job. Scoring cascade:
// (1) combined normalized leftover (compute% + memory%, lowest wins)
// (2) duration-fit (short jobs prefer tighter packing, long jobs prefer
//
//	more headroom)
//
// (3) lowest cost tier
// (4) least-recently-used
// (5) random, final tie-break among truly identical candidates
func SelectGPU(gpus []GPU, job Job, recentAvgJobSize int, rng *rand.Rand) (string, error) {
	if len(gpus) == 0 {
		return "", ErrNoGPUs
	}
	if job.Units <= 0 {
		return "", fmt.Errorf("invalid job units %d: must be positive", job.Units)
	}
	if job.MemoryGB <= 0 {
		return "", fmt.Errorf("invalid job memory %d: must be positive", job.MemoryGB)
	}
	for _, g := range gpus {
		if g.FreeCompute > g.ComputeCapacity {
			return "", fmt.Errorf("gpu %s inconsistent state: FreeCompute (%d) exceeds ComputeCapacity (%d)", g.ID, g.FreeCompute, g.ComputeCapacity)
		}
		if g.FreeMemoryGB > g.MemoryGB {
			return "", fmt.Errorf("gpu %s inconsistent state: FreeMemoryGB (%d) exceeds MemoryGB (%d)", g.ID, g.FreeMemoryGB, g.MemoryGB)
		}
	}

	allowedTiers := eligibleTiers(job.Type)
	anyEligibleTierPresent := false
	for _, g := range gpus {
		if allowedTiers[g.Tier] {
			anyEligibleTierPresent = true
			break
		}
	}
	if !anyEligibleTierPresent {
		return "", ErrNoTier
	}

	var candidates []GPU
	for _, g := range gpus {
		if !allowedTiers[g.Tier] {
			continue
		}
		if g.FreeCompute >= job.Units && g.FreeMemoryGB >= job.MemoryGB {
			candidates = append(candidates, g)
		}
	}
	if len(candidates) == 0 {
		return "", ErrNoFit
	}

	candidates = headroomPreservingCandidates(candidates, job, recentAvgJobSize)

	best := bestCombinedLeftoverCandidates(candidates, job)
	best = durationFitCandidates(best, job)
	best = lowestCostTierCandidates(best)
	best = leastRecentlyUsedCandidates(best)

	return best[rng.Intn(len(best))].ID, nil
}

// combinedLeftoverScore normalizes leftover compute and memory as
// percentages of total capacity, then sums them -- this avoids raw
// compute units (small numbers) being dwarfed or dwarfing raw memory GB
// (larger numbers). Lower score = tighter fit = better.
func combinedLeftoverScore(g GPU, job Job) float64 {
	computeLeftoverPct := float64(g.FreeCompute-job.Units) / float64(g.ComputeCapacity)
	memoryLeftoverPct := float64(g.FreeMemoryGB-job.MemoryGB) / float64(g.MemoryGB)
	return computeLeftoverPct + memoryLeftoverPct
}

func bestCombinedLeftoverCandidates(gpus []GPU, job Job) []GPU {
	minScore := combinedLeftoverScore(gpus[0], job)
	for _, g := range gpus {
		if s := combinedLeftoverScore(g, job); s < minScore {
			minScore = s
		}
	}
	var result []GPU
	for _, g := range gpus {
		if combinedLeftoverScore(g, job) == minScore {
			result = append(result, g)
		}
	}
	return result
}

// durationFitCandidates prefers tighter-fitting GPUs for short jobs
// (they'll free up soon, so packing tight is safe) and roomier GPUs for
// long jobs (avoid locking a nearly-full GPU for a long time). Short is
// defined as under 5 minutes, arbitrary but documented threshold.
const shortJobThresholdSeconds = 300

func durationFitCandidates(gpus []GPU, job Job) []GPU {
	if len(gpus) <= 1 {
		return gpus
	}
	preferTight := job.ExpectedDurationSeconds > 0 && job.ExpectedDurationSeconds <= shortJobThresholdSeconds
	preferRoomy := job.ExpectedDurationSeconds > shortJobThresholdSeconds

	if !preferTight && !preferRoomy {
		return gpus // no duration info provided, no preference
	}

	best := gpus[0]
	for _, g := range gpus[1:] {
		if preferTight && g.FreeCompute < best.FreeCompute {
			best = g
		}
		if preferRoomy && g.FreeCompute > best.FreeCompute {
			best = g
		}
	}
	var result []GPU
	for _, g := range gpus {
		if g.FreeCompute == best.FreeCompute {
			result = append(result, g)
		}
	}
	return result
}

func lowestCostTierCandidates(gpus []GPU) []GPU {
	minCost := gpus[0].CostPerUnitHour
	for _, g := range gpus {
		if g.CostPerUnitHour < minCost {
			minCost = g.CostPerUnitHour
		}
	}
	var result []GPU
	for _, g := range gpus {
		if g.CostPerUnitHour == minCost {
			result = append(result, g)
		}
	}
	return result
}

func leastRecentlyUsedCandidates(gpus []GPU) []GPU {
	oldest := gpus[0].LastUsed
	for _, g := range gpus {
		if g.LastUsed.Before(oldest) {
			oldest = g.LastUsed
		}
	}
	var result []GPU
	for _, g := range gpus {
		if g.LastUsed.Equal(oldest) {
			result = append(result, g)
		}
	}
	return result
}

// headroomPreservingCandidates biases away from candidates that would
// leave the ENTIRE cluster without any GPU holding "typical" headroom
// (free compute >= recentAvgJobSize), preferring candidates that keep at
// least one GPU roomy for whatever job-sized-like-recent-ones arrives
// next. If every candidate would eliminate cluster headroom regardless,
// no restriction is applied -- placing the job at all takes priority
// over preserving headroom that's already unavoidable to lose.
func headroomPreservingCandidates(candidates []GPU, job Job, recentAvgJobSize int) []GPU {
	if recentAvgJobSize <= 0 || len(candidates) <= 1 {
		return candidates
	}

	var preserving []GPU
	for _, g := range candidates {
		remaining := g.FreeCompute - job.Units
		keepsHeadroom := remaining >= recentAvgJobSize
		if !keepsHeadroom {
			for _, other := range candidates {
				if other.ID != g.ID && other.FreeCompute >= recentAvgJobSize {
					keepsHeadroom = true
					break
				}
			}
		}
		if keepsHeadroom {
			preserving = append(preserving, g)
		}
	}
	if len(preserving) == 0 {
		return candidates
	}
	return preserving
}

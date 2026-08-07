package scheduler

import (
	"errors"
	"fmt"
	"math/rand"
	"time"
)

// GPU represents one simulated GPU's current allocation state, as seen by
// the scoring function. This is a plain data snapshot -- callers (Phase 3)
// are responsible for building this from real cluster state.
type GPU struct {
	ID        string
	Capacity  int
	FreeUnits int
	CostTier  int       // lower = cheaper. Ties broken toward lower tier.
	LastUsed  time.Time // zero value = never used, treated as "most idle"
}

// ErrNoFit is returned when no GPU has enough free capacity for the job.
var ErrNoFit = errors.New("no GPU has sufficient free capacity")

// ErrNoGPUs is returned when the candidate GPU list itself is empty --
// distinct from ErrNoFit, which means GPUs exist but none are big enough.
var ErrNoGPUs = errors.New("no GPUs provided to select from")

// SelectGPU applies best-fit bin-packing to choose which GPU a job of the
// given size should be placed on. Tie-break order: least leftover space
// (best fit) -> lowest cost tier -> least-recently-used -> random.
//
// rng is an injectable random source so callers (tests, or production
// code wanting reproducibility) can seed it. Pass rand.New(rand.NewSource(seed))
// for deterministic behavior, or a shared *rand.Rand for production use.
func SelectGPU(gpus []GPU, jobSize int, rng *rand.Rand) (string, error) {
	if len(gpus) == 0 {
		return "", ErrNoGPUs
	}
	if jobSize <= 0 {
		return "", fmt.Errorf("invalid job size %d: must be positive", jobSize)
	}
	for _, g := range gpus {
		if g.FreeUnits > g.Capacity {
			return "", fmt.Errorf("gpu %s has inconsistent state: FreeUnits (%d) exceeds Capacity (%d)", g.ID, g.FreeUnits, g.Capacity)
		}
	}

	var candidates []GPU
	for _, g := range gpus {
		if g.FreeUnits >= jobSize {
			candidates = append(candidates, g)
		}
	}
	if len(candidates) == 0 {
		return "", ErrNoFit
	}

	best := bestFitCandidates(candidates, jobSize)
	best = lowestCostTierCandidates(best)
	best = leastRecentlyUsedCandidates(best)

	return best[rng.Intn(len(best))].ID, nil
}

func bestFitCandidates(gpus []GPU, jobSize int) []GPU {
	minLeftover := gpus[0].FreeUnits - jobSize
	for _, g := range gpus {
		if leftover := g.FreeUnits - jobSize; leftover < minLeftover {
			minLeftover = leftover
		}
	}
	var result []GPU
	for _, g := range gpus {
		if g.FreeUnits-jobSize == minLeftover {
			result = append(result, g)
		}
	}
	return result
}

func lowestCostTierCandidates(gpus []GPU) []GPU {
	minTier := gpus[0].CostTier
	for _, g := range gpus {
		if g.CostTier < minTier {
			minTier = g.CostTier
		}
	}
	var result []GPU
	for _, g := range gpus {
		if g.CostTier == minTier {
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

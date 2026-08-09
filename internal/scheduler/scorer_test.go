package scheduler

import (
	"math/rand"
	"testing"
	"time"
)

func seededRNG() *rand.Rand {
	return rand.New(rand.NewSource(42))
}

func baseGPU(id string, freeCompute, freeMemory int, tier Tier, cost float64) GPU {
	return GPU{
		ID:              id,
		ComputeCapacity: 10,
		FreeCompute:     freeCompute,
		MemoryGB:        80,
		FreeMemoryGB:    freeMemory,
		Tier:            tier,
		CostPerUnitHour: cost,
		LastUsed:        time.Now(),
	}
}

func TestSelectGPU_BestFit_ComputeAndMemory(t *testing.T) {
	gpus := []GPU{
		baseGPU("gpu-0", 3, 10, TierMid, 0.15), // perfect fit both dims
		baseGPU("gpu-1", 6, 40, TierMid, 0.15),
		baseGPU("gpu-2", 8, 60, TierMid, 0.15),
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}
	got, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0 (tightest combined fit), got %s", got)
	}
}

func TestSelectGPU_MemoryConstraintRejectsOtherwiseGoodFit(t *testing.T) {
	// gpu-0 has plenty of compute but not enough memory -- must be
	// excluded even though compute alone would be a perfect fit.
	gpus := []GPU{
		baseGPU("gpu-0", 5, 4, TierMid, 0.15), // compute fits, memory doesn't
		baseGPU("gpu-1", 5, 20, TierMid, 0.15),
	}
	job := Job{Units: 5, MemoryGB: 10, Type: JobBatchTolerant}
	got, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (only one with enough memory), got %s", got)
	}
}

func TestSelectGPU_NoFit_MemoryInsufficientEverywhere(t *testing.T) {
	gpus := []GPU{
		baseGPU("gpu-0", 10, 4, TierMid, 0.15),
		baseGPU("gpu-1", 10, 5, TierMid, 0.15),
	}
	job := Job{Units: 3, MemoryGB: 20, Type: JobBatchTolerant}
	_, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != ErrNoFit {
		t.Errorf("expected ErrNoFit, got %v", err)
	}
}

func TestSelectGPU_LatencySensitive_ExcludesEconomy(t *testing.T) {
	// Economy GPU has plenty of room, but latency-sensitive jobs must
	// never land there -- hard eligibility rule, not a soft preference.
	gpus := []GPU{
		baseGPU("gpu-0", 10, 80, TierEconomy, 0.07), // best fit numerically, but ineligible
		baseGPU("gpu-1", 3, 10, TierMid, 0.15),      // tighter fit, eligible
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobLatencySensitive}
	got, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (economy excluded for latency-sensitive), got %s", got)
	}
}

func TestSelectGPU_LatencySensitive_NoTierAvailable(t *testing.T) {
	// Only economy GPUs exist -- a latency-sensitive job must be
	// rejected outright, not silently placed on an ineligible tier.
	gpus := []GPU{
		baseGPU("gpu-0", 10, 80, TierEconomy, 0.07),
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobLatencySensitive}
	_, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != ErrNoTier {
		t.Errorf("expected ErrNoTier, got %v", err)
	}
}

func TestSelectGPU_BatchTolerant_CanUseAnyTier(t *testing.T) {
	gpus := []GPU{
		baseGPU("gpu-0", 3, 10, TierEconomy, 0.07),
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}
	got, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected batch-tolerant job to be eligible for economy tier: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0, got %s", got)
	}
}

func TestSelectGPU_TieBreak_CostTier(t *testing.T) {
	gpus := []GPU{
		baseGPU("gpu-0", 5, 20, TierMid, 0.15),
		baseGPU("gpu-1", 5, 20, TierEconomy, 0.07),
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}
	got, err := SelectGPU(gpus, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (cheaper tier, tied fit), got %s", got)
	}
}

func TestSelectGPU_TieBreak_LRU(t *testing.T) {
	now := time.Now()
	g0 := baseGPU("gpu-0", 5, 20, TierMid, 0.15)
	g0.LastUsed = now
	g1 := baseGPU("gpu-1", 5, 20, TierMid, 0.15)
	g1.LastUsed = now.Add(-1 * time.Hour)

	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}
	got, err := SelectGPU([]GPU{g0, g1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (least recently used), got %s", got)
	}
}

// Replaces the old TestSelectGPU_DurationFit_ShortJobPrefersTighterPack,
// which didn't actually isolate duration as the deciding factor -- these
// three tests construct genuine ties on combined-leftover score so
// duration-fit is provably what decides the outcome.

func TestSelectGPU_DurationFit_ShortJobPrefersTighterPack_Isolated(t *testing.T) {
	tied0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 30, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	tied1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 6, MemoryGB: 80, FreeMemoryGB: 14, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 2, MemoryGB: 10, Type: JobBatchTolerant, ExpectedDurationSeconds: 60} // short job

	s0 := combinedLeftoverScore(tied0, job)
	s1 := combinedLeftoverScore(tied1, job)
	if s0 != s1 {
		t.Fatalf("test setup invalid: scores not actually tied (%.4f vs %.4f) -- fix fixture before trusting this test", s0, s1)
	}

	got, err := SelectGPU([]GPU{tied0, tied1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0 (tighter free compute, correct for short job), got %s", got)
	}
}

func TestSelectGPU_DurationFit_LongJobPrefersRoomierPack(t *testing.T) {
	tied0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 30, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	tied1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 6, MemoryGB: 80, FreeMemoryGB: 14, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 2, MemoryGB: 10, Type: JobBatchTolerant, ExpectedDurationSeconds: 3600} // long job

	s0 := combinedLeftoverScore(tied0, job)
	s1 := combinedLeftoverScore(tied1, job)
	if s0 != s1 {
		t.Fatalf("test setup invalid: scores not actually tied (%.4f vs %.4f)", s0, s1)
	}

	got, err := SelectGPU([]GPU{tied0, tied1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (roomier free compute, correct for long job), got %s", got)
	}
}

func TestSelectGPU_DurationFit_SkippedWhenUnspecified(t *testing.T) {
	tied0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 30, Tier: TierMid, CostPerUnitHour: 0.20, LastUsed: time.Now()}
	tied1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 6, MemoryGB: 80, FreeMemoryGB: 14, Tier: TierEconomy, CostPerUnitHour: 0.07, LastUsed: time.Now()}
	job := Job{Units: 2, MemoryGB: 10, Type: JobBatchTolerant} // no duration specified

	s0 := combinedLeftoverScore(tied0, job)
	s1 := combinedLeftoverScore(tied1, job)
	if s0 != s1 {
		t.Fatalf("test setup invalid: scores not actually tied (%.4f vs %.4f)", s0, s1)
	}

	got, err := SelectGPU([]GPU{tied0, tied1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (cost tier decides when duration unspecified), got %s", got)
	}
}

func TestSelectGPU_InputValidation(t *testing.T) {
	gpus := []GPU{baseGPU("gpu-0", 5, 20, TierMid, 0.15)}

	if _, err := SelectGPU(nil, Job{Units: 1, MemoryGB: 1}, 0, seededRNG()); err != ErrNoGPUs {
		t.Errorf("expected ErrNoGPUs for nil list, got %v", err)
	}
	if _, err := SelectGPU(gpus, Job{Units: 0, MemoryGB: 1}, 0, seededRNG()); err == nil {
		t.Error("expected error for zero units, got nil")
	}
	if _, err := SelectGPU(gpus, Job{Units: 1, MemoryGB: 0}, 0, seededRNG()); err == nil {
		t.Error("expected error for zero memory, got nil")
	}
}

func TestSelectGPU_InconsistentState(t *testing.T) {
	bad := baseGPU("gpu-0", 15, 20, TierMid, 0.15) // FreeCompute > ComputeCapacity
	_, err := SelectGPU([]GPU{bad}, Job{Units: 1, MemoryGB: 1}, 0, seededRNG())
	if err == nil {
		t.Error("expected error for FreeCompute exceeding ComputeCapacity, got nil")
	}
}

func TestSelectGPU_ThreeWayTie_DistributesAcrossCandidates(t *testing.T) {
	now := time.Now()
	gpus := []GPU{
		baseGPU("gpu-0", 5, 20, TierMid, 0.15),
		baseGPU("gpu-1", 5, 20, TierMid, 0.15),
		baseGPU("gpu-2", 5, 20, TierMid, 0.15),
	}
	for i := range gpus {
		gpus[i].LastUsed = now
	}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}

	seen := map[string]bool{}
	for seed := int64(0); seed < 50; seed++ {
		got, err := SelectGPU(gpus, job, 0, rand.New(rand.NewSource(seed)))
		if err != nil {
			t.Fatalf("unexpected error on seed %d: %v", seed, err)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected random tie-break to distribute across multiple GPUs, only saw: %v", seen)
	}
}
func TestSelectGPU_Headroom_ActuallyOverridesPlainBestFit(t *testing.T) {
	// Constructed so plain best-fit (no headroom bias) picks gpu-0 --
	// despite gpu-0 having MORE raw free compute, its combined score
	// (compute+memory leftover) beats gpu-1's because gpu-1's memory
	// leftover is poor. gpu-0 is also the only GPU currently "roomy"
	// (free compute >= recentAvgJobSize). If gpu-0 is chosen, it drops
	// below the headroom threshold AND gpu-1 was never roomy either --
	// so choosing gpu-0 eliminates cluster headroom entirely, while
	// choosing gpu-1 preserves it via the untouched gpu-0.
	gpu0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 6, MemoryGB: 80, FreeMemoryGB: 15, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	gpu1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 70, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}

	// Confirm plain best-fit (headroom disabled) picks gpu-0, as designed.
	plain, err := SelectGPU([]GPU{gpu0, gpu1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if plain != "gpu-0" {
		t.Fatalf("test setup invalid: expected plain best-fit to pick gpu-0, got %s -- fix fixture", plain)
	}

	// With headroom bias active (recentAvgJobSize=6), the outcome should
	// flip to gpu-1, since choosing gpu-0 would eliminate all cluster
	// headroom while choosing gpu-1 preserves it via untouched gpu-0.
	withHeadroom, err := SelectGPU([]GPU{gpu0, gpu1}, job, 6, seededRNG())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if withHeadroom != "gpu-1" {
		t.Errorf("expected headroom bias to override plain best-fit and pick gpu-1, got %s", withHeadroom)
	}
}

func TestSelectGPU_Headroom_ActuallyChangesOutcome(t *testing.T) {
	// Construct a case where placing on the tightest-fit GPU eliminates
	// headroom cluster-wide, but an alternative preserves it. Only 2
	// GPUs total, both would be touched or already tight, so headroom
	// can only survive via the non-tightest choice.
	gpu0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 3, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	gpu1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}
	// gpu-0 after placement: 0 free. gpu-1 has 4 free (untouched) -- if
	// recentAvgJobSize=5, gpu-1's 4 free does NOT count as headroom
	// either. So placing on gpu-0 leaves cluster headroom-less (0 and 4,
	// both < 5). Placing on gpu-1 leaves gpu-0 untouched at 3 (still <5)
	// and gpu-1 at 1 (<5) -- ALSO headroom-less. Neither preserves
	// headroom -> heuristic should fall back to normal best-fit (gpu-0,
	// tighter numeric fit).
	got, err := SelectGPU([]GPU{gpu0, gpu1}, job, 5, seededRNG())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected fallback to plain best-fit (gpu-0) when no candidate can preserve headroom, got %s", got)
	}
}

func TestSelectGPU_Headroom_PrefersPreservingCandidate(t *testing.T) {
	// gpu-0: tight fit (3 free, job needs 3 -> 0 left). gpu-1: roomier
	// (10 free). recentAvgJobSize=5. Placing on gpu-0 leaves gpu-1
	// untouched at 10 free (>=5) -- headroom IS preserved via gpu-1.
	// Placing on gpu-1 (10->7 free) also preserves headroom via gpu-1
	// itself. Both preserve headroom here -- so best-fit (gpu-0) should
	// still win, since headroom doesn't need to exclude anything. This
	// confirms the heuristic doesn't interfere when headroom isn't at risk.
	gpu0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 3, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	gpu1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 10, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}

	got, err := SelectGPU([]GPU{gpu0, gpu1}, job, 5, seededRNG())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0 (best fit, headroom already safe via untouched gpu-1), got %s", got)
	}
}

func TestSelectGPU_Headroom_DisabledWhenZero(t *testing.T) {
	// recentAvgJobSize=0 must behave identically to no headroom logic
	// at all -- pure best-fit, regardless of headroom consequences.
	gpu0 := GPU{ID: "gpu-0", ComputeCapacity: 10, FreeCompute: 3, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	gpu1 := GPU{ID: "gpu-1", ComputeCapacity: 10, FreeCompute: 4, MemoryGB: 80, FreeMemoryGB: 40, Tier: TierMid, CostPerUnitHour: 0.15, LastUsed: time.Now()}
	job := Job{Units: 3, MemoryGB: 10, Type: JobBatchTolerant}

	got, err := SelectGPU([]GPU{gpu0, gpu1}, job, 0, seededRNG())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0 (plain best-fit with headroom disabled), got %s", got)
	}
}

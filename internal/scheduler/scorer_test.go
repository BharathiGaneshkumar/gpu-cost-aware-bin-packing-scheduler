package scheduler

import (
	"math/rand"
	"testing"
	"time"
)

// seededRNG returns a deterministic random source so tests are
// reproducible even when exercising the random tie-break path.
func seededRNG() *rand.Rand {
	return rand.New(rand.NewSource(42))
}

func TestSelectGPU_BestFit(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 3, CostTier: 1},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 6, CostTier: 1},
		{ID: "gpu-2", Capacity: 10, FreeUnits: 8, CostTier: 1},
	}
	got, err := SelectGPU(gpus, 3, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0 (best fit), got %s", got)
	}
}

func TestSelectGPU_NoFit(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 3, CostTier: 1},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1},
	}
	_, err := SelectGPU(gpus, 8, seededRNG())
	if err != ErrNoFit {
		t.Errorf("expected ErrNoFit, got %v", err)
	}
}

func TestSelectGPU_TieBreak_CostTier(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 2},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1},
	}
	got, err := SelectGPU(gpus, 3, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (cheaper tier, tied fit), got %s", got)
	}
}

func TestSelectGPU_TieBreak_LRU(t *testing.T) {
	now := time.Now()
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now.Add(-1 * time.Hour)},
	}
	got, err := SelectGPU(gpus, 3, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (least recently used), got %s", got)
	}
}

func TestSelectGPU_ExactFit(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 4, CostTier: 1},
	}
	got, err := SelectGPU(gpus, 4, seededRNG())
	if err != nil {
		t.Fatalf("expected exact fit to succeed, got error: %v", err)
	}
	if got != "gpu-0" {
		t.Errorf("expected gpu-0, got %s", got)
	}
}

func TestSelectGPU_ZeroFreeGPUExcluded(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 0, CostTier: 1},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 2, CostTier: 1},
	}
	got, err := SelectGPU(gpus, 2, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	if got != "gpu-1" {
		t.Errorf("expected gpu-1 (only real fit), got %s", got)
	}
}

// --- New tests covering previously-missing failure modes ---

func TestSelectGPU_EmptyGPUList(t *testing.T) {
	_, err := SelectGPU([]GPU{}, 3, seededRNG())
	if err != ErrNoGPUs {
		t.Errorf("expected ErrNoGPUs for empty list, got %v", err)
	}
}

func TestSelectGPU_NilGPUList(t *testing.T) {
	// nil slice should behave identically to an empty one -- Go allows
	// ranging over nil slices safely, but worth confirming explicitly.
	_, err := SelectGPU(nil, 3, seededRNG())
	if err != ErrNoGPUs {
		t.Errorf("expected ErrNoGPUs for nil list, got %v", err)
	}
}

func TestSelectGPU_ZeroJobSize(t *testing.T) {
	gpus := []GPU{{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1}}
	_, err := SelectGPU(gpus, 0, seededRNG())
	if err == nil {
		t.Error("expected an error for zero job size, got nil")
	}
}

func TestSelectGPU_NegativeJobSize(t *testing.T) {
	gpus := []GPU{{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1}}
	_, err := SelectGPU(gpus, -3, seededRNG())
	if err == nil {
		t.Error("expected an error for negative job size, got nil")
	}
}

func TestSelectGPU_AllGPUsFull(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 0, CostTier: 1},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 0, CostTier: 1},
		{ID: "gpu-2", Capacity: 10, FreeUnits: 0, CostTier: 1},
	}
	_, err := SelectGPU(gpus, 1, seededRNG())
	if err != ErrNoFit {
		t.Errorf("expected ErrNoFit when entire fleet is full, got %v", err)
	}
}

func TestSelectGPU_InconsistentState_FreeExceedsCapacity(t *testing.T) {
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 15, CostTier: 1}, // corrupted data
	}
	_, err := SelectGPU(gpus, 3, seededRNG())
	if err == nil {
		t.Error("expected an error for FreeUnits exceeding Capacity, got nil")
	}
}

func TestSelectGPU_ThreeWayTie_ReturnsValidCandidate(t *testing.T) {
	// Fit, cost tier, AND LastUsed all identical across all 3 GPUs --
	// should still return a valid answer, not crash, and it must be one
	// of the actual candidates.
	now := time.Now()
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-2", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
	}
	got, err := SelectGPU(gpus, 3, seededRNG())
	if err != nil {
		t.Fatalf("expected a fit, got error: %v", err)
	}
	valid := map[string]bool{"gpu-0": true, "gpu-1": true, "gpu-2": true}
	if !valid[got] {
		t.Errorf("got %s, expected one of gpu-0/gpu-1/gpu-2", got)
	}
}

func TestSelectGPU_ThreeWayTie_DistributesAcrossCandidates(t *testing.T) {
	// Run the truly-identical scenario many times with different seeds
	// and confirm the random tie-break actually visits more than one
	// candidate -- catches a bug where randomness silently always picks
	// the same index (e.g. a modulo error or an unshuffled slice).
	now := time.Now()
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-2", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
	}
	seen := map[string]bool{}
	for seed := int64(0); seed < 50; seed++ {
		rng := rand.New(rand.NewSource(seed))
		got, err := SelectGPU(gpus, 3, rng)
		if err != nil {
			t.Fatalf("unexpected error on seed %d: %v", seed, err)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Errorf("expected random tie-break to distribute across multiple GPUs over 50 seeds, only saw: %v", seen)
	}
}

func TestSelectGPU_Deterministic_SameSeedSameResult(t *testing.T) {
	// Same seed should always produce the same result -- confirms our
	// injectable rng actually makes behavior reproducible for testing.
	now := time.Now()
	gpus := []GPU{
		{ID: "gpu-0", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
		{ID: "gpu-1", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: now},
	}
	got1, _ := SelectGPU(gpus, 3, rand.New(rand.NewSource(7)))
	got2, _ := SelectGPU(gpus, 3, rand.New(rand.NewSource(7)))
	if got1 != got2 {
		t.Errorf("same seed produced different results: %s vs %s", got1, got2)
	}
}

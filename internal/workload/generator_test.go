package workload

import (
	"testing"
)

func TestGenerateBatch_Deterministic(t *testing.T) {
	batch1 := GenerateBatch(50, 42)
	batch2 := GenerateBatch(50, 42)
	for i := range batch1 {
		if batch1[i].Units != batch2[i].Units {
			t.Fatalf("same seed produced different batches at index %d: %d vs %d", i, batch1[i].Units, batch2[i].Units)
		}
	}
}

func TestGenerateBatch_DistributionSanityCheck(t *testing.T) {
	batch := GenerateBatch(1000, 42)
	small, medium, large := 0, 0, 0
	for _, j := range batch {
		switch {
		case j.Units <= 3:
			small++
		case j.Units <= 6:
			medium++
		default:
			large++
		}
	}
	t.Logf("distribution over 1000 jobs: small=%d medium=%d large=%d", small, medium, large)

	// Loose bounds -- not exact, since it's random, just sanity-checking
	// the distribution roughly matches our intended 60/30/10 weights.
	if small < 500 || small > 700 {
		t.Errorf("expected roughly 600 small jobs, got %d", small)
	}
	if large < 50 || large > 150 {
		t.Errorf("expected roughly 100 large jobs, got %d", large)
	}
}
func TestGenerateBatch_JobTypeDistribution(t *testing.T) {
	batch := GenerateBatch(1000, 42)
	batchTolerant, latencySensitive := 0, 0
	for _, j := range batch {
		switch j.Type {
		case "batch-tolerant":
			batchTolerant++
		case "latency-sensitive":
			latencySensitive++
		default:
			t.Errorf("unexpected job type: %q", j.Type)
		}
	}
	t.Logf("job type distribution over 1000 jobs: batch-tolerant=%d latency-sensitive=%d", batchTolerant, latencySensitive)

	// Loose bounds around the intended 60/40 split.
	if batchTolerant < 540 || batchTolerant > 660 {
		t.Errorf("expected roughly 600 batch-tolerant jobs, got %d", batchTolerant)
	}
}

func TestGenerateBatch_DurationDistribution(t *testing.T) {
	batch := GenerateBatch(1000, 42)
	short, long := 0, 0
	for _, j := range batch {
		if j.ExpectedDurationSeconds <= shortJobMaxSeconds {
			short++
		} else {
			long++
		}
		if j.ExpectedDurationSeconds < shortJobMinSeconds || j.ExpectedDurationSeconds > longJobMaxSeconds {
			t.Errorf("duration %d out of expected overall range [%d, %d]", j.ExpectedDurationSeconds, shortJobMinSeconds, longJobMaxSeconds)
		}
	}
	t.Logf("duration distribution over 1000 jobs: short=%d long=%d", short, long)

	if short < 400 || short > 600 {
		t.Errorf("expected roughly 500 short jobs, got %d", short)
	}
}

func TestGenerateBatch_MemoryCorrelatesWithUnits(t *testing.T) {
	batch := GenerateBatch(1000, 42)
	for _, j := range batch {
		if j.MemoryGB < 1 {
			t.Errorf("job %s has invalid MemoryGB %d (must be >= 1)", j.Name, j.MemoryGB)
		}
		// Memory should be roughly proportional to units -- sanity check
		// it's not wildly disconnected (e.g. a 1-unit job needing 500GB).
		maxPlausible := j.Units*4 + 3 // upper bound given generation formula
		if j.MemoryGB > maxPlausible {
			t.Errorf("job %s: MemoryGB %d implausibly high for %d units (max plausible ~%d)", j.Name, j.MemoryGB, j.Units, maxPlausible)
		}
	}
}

func TestGenerateBatch_MemoryDeterministic(t *testing.T) {
	batch1 := GenerateBatch(50, 42)
	batch2 := GenerateBatch(50, 42)
	for i := range batch1 {
		if batch1[i].MemoryGB != batch2[i].MemoryGB {
			t.Fatalf("same seed produced different MemoryGB at index %d: %d vs %d", i, batch1[i].MemoryGB, batch2[i].MemoryGB)
		}
		if batch1[i].Type != batch2[i].Type {
			t.Fatalf("same seed produced different Type at index %d: %s vs %s", i, batch1[i].Type, batch2[i].Type)
		}
		if batch1[i].ExpectedDurationSeconds != batch2[i].ExpectedDurationSeconds {
			t.Fatalf("same seed produced different duration at index %d: %d vs %d", i, batch1[i].ExpectedDurationSeconds, batch2[i].ExpectedDurationSeconds)
		}
	}
}

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

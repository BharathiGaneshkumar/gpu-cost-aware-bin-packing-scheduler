package clusterstate

import (
	"testing"
	"time"
)

func TestAddAndActiveForGPU(t *testing.T) {
	l := NewReservationLedger()
	l.Add("gpu-2", 4, 16, "job-a")
	l.Add("gpu-2", 3, 8, "job-b")
	l.Add("gpu-3", 5, 20, "job-c")

	active := l.ActiveForGPU("gpu-2")
	if len(active) != 2 {
		t.Fatalf("expected 2 active reservations for gpu-2, got %d", len(active))
	}

	activeGPU3 := l.ActiveForGPU("gpu-3")
	if len(activeGPU3) != 1 {
		t.Fatalf("expected 1 active reservation for gpu-3, got %d", len(activeGPU3))
	}

	noReservations := l.ActiveForGPU("gpu-99")
	if len(noReservations) != 0 {
		t.Fatalf("expected 0 reservations for untouched gpu, got %d", len(noReservations))
	}
}

func TestReservationExpiry(t *testing.T) {
	l := NewReservationLedger()
	l.Add("gpu-1", 4, 16, "job-old")

	// Manually backdate the reservation past the timeout to simulate expiry
	// without sleeping 60s in a test.
	l.mu.Lock()
	l.reservations["gpu-1"][0].CreatedAt = time.Now().Add(-2 * reservationTimeout)
	l.mu.Unlock()

	active := l.ActiveForGPU("gpu-1")
	if len(active) != 0 {
		t.Fatalf("expected expired reservation to be excluded, got %d active", len(active))
	}
}

func TestClearByPodName(t *testing.T) {
	l := NewReservationLedger()
	l.Add("gpu-4", 4, 16, "job-x")
	l.Add("gpu-4", 2, 8, "job-y")

	l.ClearByPodName("gpu-4", "job-x")

	active := l.ActiveForGPU("gpu-4")
	if len(active) != 1 {
		t.Fatalf("expected 1 remaining reservation after clear, got %d", len(active))
	}
	if active[0].PodName != "job-y" {
		t.Fatalf("expected remaining reservation to be job-y, got %s", active[0].PodName)
	}
}

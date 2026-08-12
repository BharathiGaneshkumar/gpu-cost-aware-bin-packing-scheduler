package clusterstate

import (
	"sync"
	"time"
)

// Reservation represents a pending GPU claim made by the webhook
// at admission time, before the pod has reached Running (or even
// appeared in the API server's pod listing in some cases).
type Reservation struct {
	GPUID        string
	ComputeUnits int
	MemoryGB     int
	PodName      string
	CreatedAt    time.Time
}

const reservationTimeout = 60 * time.Second

type ReservationLedger struct {
	mu           sync.Mutex
	reservations map[string][]Reservation // keyed by GPUID
}

func NewReservationLedger() *ReservationLedger {
	return &ReservationLedger{
		reservations: make(map[string][]Reservation),
	}
}

// Add records a new reservation immediately after a placement decision.
func (l *ReservationLedger) Add(gpuID string, computeUnits, memoryGB int, podName string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.reservations[gpuID] = append(l.reservations[gpuID], Reservation{
		GPUID:        gpuID,
		ComputeUnits: computeUnits,
		MemoryGB:     memoryGB,
		PodName:      podName,
		CreatedAt:    time.Now(),
	})
}

// ActiveForGPU returns non-expired reservations for a given GPU.
// Expiry here is timeout-only; event-driven clearing (ClearIfRealized)
// happens separately once we wire this into FetchClusterState.
func (l *ReservationLedger) ActiveForGPU(gpuID string) []Reservation {
	l.mu.Lock()
	defer l.mu.Unlock()

	var active []Reservation
	for _, r := range l.reservations[gpuID] {
		if time.Since(r.CreatedAt) < reservationTimeout {
			active = append(active, r)
		}
	}
	return active
}

// ClearByPodName removes a reservation once the real pod has been
// observed in cluster state (any phase), since real state has caught up.
func (l *ReservationLedger) ClearByPodName(gpuID, podName string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	remaining := l.reservations[gpuID][:0]
	for _, r := range l.reservations[gpuID] {
		if r.PodName != podName {
			remaining = append(remaining, r)
		}
	}
	l.reservations[gpuID] = remaining
}

package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"gpu-bin-packing-scheduler/internal/clusterstate"
)

const (
	resourcePrefix   = "simulated.com/gpu-"
	memAnnotationKey = "gpu-memory-allocated"
	tierLabelKey     = "gpu-tier"
	gpuIDLabelKey    = "gpu-id"
	computeCapacity  = 10
)

type tierSpec struct {
	memoryGB        int
	costPerUnitHour float64
}

var tierSpecs = map[Tier]tierSpec{
	TierPremium: {memoryGB: 80, costPerUnitHour: 0.30},
	TierMid:     {memoryGB: 40, costPerUnitHour: 0.15},
	TierEconomy: {memoryGB: 16, costPerUnitHour: 0.07},
}

// FetchClusterState queries live cluster state. Compute/memory usage is
// counted from any pod (Running OR Pending) that already carries our GPU
// resource requests, since a Pending-but-mutated pod represents a real,
// already-decided claim even before Kubernetes starts it. The reservation
// ledger only needs to cover the narrower window before the pod object
// even exists in the API server yet.
func FetchClusterState(ctx context.Context, clientset *kubernetes.Clientset, ledger *clusterstate.ReservationLedger) ([]GPU, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: gpuIDLabelKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list GPU nodes: %w", err)
	}

	allPods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	usedCompute := make(map[string]int)
	usedMemory := make(map[string]int)
	seenPodNames := make(map[string]bool)

	for _, pod := range allPods.Items {
		seenPodNames[pod.Name] = true

		// Only count pods that are still active (not terminal) and have
		// actually been mutated with a GPU resource request. This covers
		// both Running and Pending-but-scheduled pods.
		if pod.Status.Phase == "Succeeded" || pod.Status.Phase == "Failed" {
			continue
		}

		hasGPURequest := false
		for _, container := range pod.Spec.Containers {
			for resName, qty := range container.Resources.Requests {
				name := string(resName)
				if !strings.HasPrefix(name, resourcePrefix) {
					continue
				}
				gpuID := strings.TrimPrefix(name, resourcePrefix)
				usedCompute[gpuID] += int(qty.Value())
				hasGPURequest = true
			}
		}
		if !hasGPURequest {
			continue
		}
		if memStr, ok := pod.Annotations[memAnnotationKey]; ok {
			if memVal, err := strconv.Atoi(memStr); err == nil {
				gpuID, ok := pod.Annotations["gpu-allocated-id"]
				if ok {
					usedMemory[gpuID] += memVal
				}
			}
		}
	}

	var gpus []GPU
	for _, node := range nodes.Items {
		gpuID, ok := node.Labels[gpuIDLabelKey]
		if !ok {
			continue
		}
		tierStr, ok := node.Labels[tierLabelKey]
		if !ok {
			return nil, fmt.Errorf("node %s has gpu-id label but no gpu-tier label", node.Name)
		}
		tier := Tier(tierStr)
		spec, ok := tierSpecs[tier]
		if !ok {
			return nil, fmt.Errorf("node %s has unknown gpu-tier value %q", node.Name, tierStr)
		}

		// Reconcile reservations: once a reserved pod is visible in the API
		// server at all (any phase), the loop above already counts its real
		// usage if it has GPU requests, so the reservation is redundant and
		// safe to clear. Only pods that don't exist yet in the API server
		// still need to be covered by the reservation.
		reservedCompute := 0
		reservedMemory := 0
		if ledger != nil {
			for _, r := range ledger.ActiveForGPU(gpuID) {
				if seenPodNames[r.PodName] {
					ledger.ClearByPodName(gpuID, r.PodName)
					continue
				}
				reservedCompute += r.ComputeUnits
				reservedMemory += r.MemoryGB
			}
		}

		gpus = append(gpus, GPU{
			ID:              gpuID,
			ComputeCapacity: computeCapacity,
			FreeCompute:     computeCapacity - usedCompute[gpuID] - reservedCompute,
			MemoryGB:        spec.memoryGB,
			FreeMemoryGB:    spec.memoryGB - usedMemory[gpuID] - reservedMemory,
			Tier:            tier,
			CostPerUnitHour: spec.costPerUnitHour,
			LastUsed:        time.Now(),
		})
	}
	return gpus, nil
}

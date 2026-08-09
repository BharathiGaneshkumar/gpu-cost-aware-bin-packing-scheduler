package scheduler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	resourcePrefix = "simulated.com/gpu-"
	gpuCapacity    = 10
	numGPUs        = 4

	// TEMPORARY placeholder defaults until Chunk 3 wires in real
	// per-node tier/memory data read from node labels.
	placeholderMemoryGB = 40
	placeholderTier     = TierMid
	placeholderCost     = 0.15
)

// FetchClusterState queries the live cluster for current GPU allocation
// state, computing free compute units per simulated GPU by subtracting
// all currently-requested units from each GPU's known fixed capacity.
//
// NOTE: memory/tier/cost fields are placeholder-uniform values for now --
// Chunk 3 will replace this with real per-node data from node labels.
func FetchClusterState(ctx context.Context, clientset *kubernetes.Clientset) ([]GPU, error) {
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	used := make(map[string]int) // gpu ID -> compute units currently claimed

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			for resName, qty := range container.Resources.Requests {
				name := string(resName)
				if !strings.HasPrefix(name, resourcePrefix) {
					continue
				}
				gpuID := strings.TrimPrefix(name, resourcePrefix)
				units := int(qty.Value())
				used[gpuID] += units
			}
		}
	}

	var gpus []GPU
	for i := 0; i < numGPUs; i++ {
		id := strconv.Itoa(i)
		gpus = append(gpus, GPU{
			ID:              id,
			ComputeCapacity: gpuCapacity,
			FreeCompute:     gpuCapacity - used[id],
			MemoryGB:        placeholderMemoryGB,
			FreeMemoryGB:    placeholderMemoryGB, // TODO Chunk 3: subtract real memory usage
			Tier:            placeholderTier,
			CostPerUnitHour: placeholderCost,
			LastUsed:        time.Now(),
		})
	}
	return gpus, nil
}

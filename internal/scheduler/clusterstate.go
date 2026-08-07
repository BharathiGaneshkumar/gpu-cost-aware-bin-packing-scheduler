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
)

// FetchClusterState queries the live cluster for current GPU allocation
// state, computing free units per simulated GPU by subtracting all
// currently-requested units from each GPU's known fixed capacity.
func FetchClusterState(ctx context.Context, clientset *kubernetes.Clientset) ([]GPU, error) {
	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	used := make(map[string]int) // gpu ID -> units currently claimed

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
			ID:        id,
			Capacity:  gpuCapacity,
			FreeUnits: gpuCapacity - used[id],
			CostTier:  1, // uniform for now -- tier extension comes later
			LastUsed:  time.Now(),
		})
	}
	return gpus, nil
}

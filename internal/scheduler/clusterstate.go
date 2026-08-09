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
	resourcePrefix   = "simulated.com/gpu-"
	memAnnotationKey = "gpu-memory-allocated"
	tierLabelKey     = "gpu-tier"
	gpuIDLabelKey    = "gpu-id"
	computeCapacity  = 10
)

// tierSpec defines the fixed properties of a GPU tier -- memory capacity
// and hourly cost, loosely modeled on real H100/A100/T4-L40S pricing.
type tierSpec struct {
	memoryGB        int
	costPerUnitHour float64
}

var tierSpecs = map[Tier]tierSpec{
	TierPremium: {memoryGB: 80, costPerUnitHour: 0.30},
	TierMid:     {memoryGB: 40, costPerUnitHour: 0.15},
	TierEconomy: {memoryGB: 16, costPerUnitHour: 0.07},
}

// FetchClusterState queries the live cluster for current GPU allocation
// state: real compute usage comes from Kubernetes' own resource
// accounting (simulated.com/gpu-N is a real, enforced resource type).
// Memory usage is NOT a real Kubernetes resource in this simulation --
// it's self-tracked via a "gpu-memory-allocated" annotation the webhook
// stamps on each pod it places, summed here per node.
func FetchClusterState(ctx context.Context, clientset *kubernetes.Clientset) ([]GPU, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: gpuIDLabelKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list GPU nodes: %w", err)
	}

	pods, err := clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	usedCompute := make(map[string]int) // gpu ID -> compute units used
	usedMemory := make(map[string]int)  // gpu ID -> memory GB used (self-tracked)

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			for resName, qty := range container.Resources.Requests {
				name := string(resName)
				if !strings.HasPrefix(name, resourcePrefix) {
					continue
				}
				gpuID := strings.TrimPrefix(name, resourcePrefix)
				usedCompute[gpuID] += int(qty.Value())
			}
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

		gpus = append(gpus, GPU{
			ID:              gpuID,
			ComputeCapacity: computeCapacity,
			FreeCompute:     computeCapacity - usedCompute[gpuID],
			MemoryGB:        spec.memoryGB,
			FreeMemoryGB:    spec.memoryGB - usedMemory[gpuID],
			Tier:            tier,
			CostPerUnitHour: spec.costPerUnitHour,
			LastUsed:        time.Now(),
		})
	}
	return gpus, nil
}

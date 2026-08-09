package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"gpu-bin-packing-scheduler/internal/workload"
)

const unitsPerGPU = 10

func main() {
	kubeContext := flag.String("context", "", "kubeconfig context to target (required)")
	seed := flag.Int64("seed", 42, "seed for reproducible workload generation")
	n := flag.Int("n", 30, "number of jobs to generate")
	flag.Parse()

	if *kubeContext == "" {
		log.Fatal("--context is required")
	}

	clientset, err := buildClientset(*kubeContext)
	if err != nil {
		log.Fatalf("failed to build clientset: %v", err)
	}

	jobs := workload.GenerateBatch(*n, *seed)
	fmt.Printf("Generated %d jobs (seed=%d)\n", len(jobs), *seed)

	const totalClusterCapacity = 40 // 4 GPUs x 10 units each
	totalDemand := 0
	for _, j := range jobs {
		totalDemand += j.Units
	}
	fmt.Printf("Total demand: %d units (cluster capacity: %d units)\n", totalDemand, totalClusterCapacity)
	if totalDemand > totalClusterCapacity {
		log.Fatalf("total demand (%d) exceeds cluster capacity (%d) -- any Pending result would be a capacity failure, not a fragmentation failure. Reduce -n or use a different seed.", totalDemand, totalClusterCapacity)
	}

	ctx := context.Background()
	for _, j := range jobs {
		if err := submitSharedResourceJob(ctx, clientset, j); err != nil {
			log.Printf("failed to submit %s: %v", j.Name, err)
		}
	}

	fmt.Println("Submitted all jobs. Waiting 15s for scheduling to settle...")
	time.Sleep(15 * time.Second)

	printResults(ctx, clientset, jobs)

	nodeNames := []string{}
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{
		LabelSelector: "gpu-id",
	})
	if err != nil {
		log.Printf("failed to list GPU nodes: %v", err)
	} else {
		for _, n := range nodes.Items {
			nodeNames = append(nodeNames, n.Name)
		}
	}
	measureFragmentation(ctx, clientset, nodeNames)
}

// buildClientset loads ~/.kube/config and builds a clientset targeting
// the given context, the same way kubectl itself connects to a cluster.
func buildClientset(kubeContext string) (*kubernetes.Clientset, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	configOverrides := &clientcmd.ConfigOverrides{CurrentContext: kubeContext}
	kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)

	config, err := kubeConfig.ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(config)
}

// submitSharedResourceJob creates a pod requesting units of the shared
// simulated.com/gpu-capacity resource -- used for LeastAllocated /
// MostAllocated baseline runs where the real K8s scheduler picks the node.
func submitSharedResourceJob(ctx context.Context, clientset *kubernetes.Clientset, j workload.Job) error {
	qty := resourceapi.MustParse(fmt.Sprintf("%d", j.Units))
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: j.Name},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:    "workload",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{"simulated.com/gpu-capacity": qty},
						Limits:   corev1.ResourceList{"simulated.com/gpu-capacity": qty},
					},
				},
			},
		},
	}
	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func printResults(ctx context.Context, clientset *kubernetes.Clientset, jobs []workload.Job) {
	running, pending := 0, 0
	for _, j := range jobs {
		pod, err := clientset.CoreV1().Pods("default").Get(ctx, j.Name, metav1.GetOptions{})
		if err != nil {
			fmt.Printf("%s: ERROR fetching pod: %v\n", j.Name, err)
			continue
		}
		status := string(pod.Status.Phase)
		node := pod.Spec.NodeName
		if node == "" {
			node = "(unscheduled)"
		}
		fmt.Printf("%s (units=%d): %s on %s\n", j.Name, j.Units, status, node)
		switch pod.Status.Phase {
		case corev1.PodRunning:
			running++
		case corev1.PodPending:
			pending++
		}
	}
	fmt.Printf("\n--- Summary: %d Running, %d Pending, %d total ---\n", running, pending, len(jobs))
}

// measureFragmentation computes, per node, how many units of the shared
// gpu-capacity resource are currently free, and reports the maximum
// single-node free capacity -- the real fragmentation signal, since it
// answers "how big a job could still be scheduled right now," independent
// of whether the current batch happened to all fit.
func measureFragmentation(ctx context.Context, clientset *kubernetes.Clientset, nodeNames []string) {
	allPods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{
		FieldSelector: "status.phase=Running",
	})
	if err != nil {
		log.Printf("failed to list pods for fragmentation measurement: %v", err)
		return
	}

	usedPerNode := make(map[string]int)
	for _, pod := range allPods.Items {
		if pod.Spec.NodeName == "" {
			continue
		}
		for _, c := range pod.Spec.Containers {
			if qty, ok := c.Resources.Requests["simulated.com/gpu-capacity"]; ok {
				usedPerNode[pod.Spec.NodeName] += int(qty.Value())
			}
		}
	}

	fmt.Println("\n--- Fragmentation snapshot ---")
	maxFree := 0
	totalFree := 0
	for _, node := range nodeNames {
		free := unitsPerGPU - usedPerNode[node]
		totalFree += free
		if free > maxFree {
			maxFree = free
		}
		fmt.Printf("%s: %d/%d free\n", node, free, unitsPerGPU)
	}
	fmt.Printf("Total free across cluster: %d units\n", totalFree)
	fmt.Printf("Max free on any single GPU: %d units  <- largest job this cluster could still accept right now\n", maxFree)
}

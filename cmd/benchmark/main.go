package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourceapi "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"gpu-bin-packing-scheduler/internal/workload"
)

const (
	numGPUsPerTier      = 2
	computeCapacityEach = 10

	premiumMemGB = 80
	midMemGB     = 40
	economyMemGB = 16
	premiumCost  = 0.30
	midCost      = 0.15
	economyCost  = 0.07
)

var totalComputeCapacity = numGPUsPerTier * 3 * computeCapacityEach

type RunResult struct {
	Strategy            string    `json:"strategy"`
	Mode                string    `json:"mode"`
	LoadLevelPct        int       `json:"load_level_pct"`
	Seed                int64     `json:"seed"`
	Timestamp           time.Time `json:"timestamp"`
	TotalJobs           int       `json:"total_jobs"`
	TotalDemandUnits    int       `json:"total_demand_units"`
	Scheduled           int       `json:"scheduled"`
	Pending             int       `json:"pending"`
	DeniedByWebhook     int       `json:"denied_by_webhook"`
	TotalScheduledUnits int       `json:"total_scheduled_units"`
	SubmissionErrors    int       `json:"submission_errors"`
	MaxFreeCompute      int       `json:"max_free_compute"`
	TotalFreeCompute    int       `json:"total_free_compute"`
	UtilizationPct      float64   `json:"utilization_pct"`
	IdleDollarsWasted   float64   `json:"idle_dollars_wasted"`
	CostPerJob          float64   `json:"cost_per_job"`
	ProbeJobSucceeded   bool      `json:"probe_job_succeeded"`
	Valid               bool      `json:"valid"`
	Notes               string    `json:"notes,omitempty"`
}

func main() {
	kubeContext := flag.String("context", "", "kubeconfig context to target (required)")
	mode := flag.String("mode", "", "submission mode: shared|webhook (required)")
	strategyName := flag.String("strategy", "", "label for this strategy in results, e.g. LeastAllocated (required)")
	loadLevels := flag.String("loadLevels", "50,75,90,110", "comma-separated load levels as % of total compute capacity")
	seedCount := flag.Int("seedCount", 10, "number of seeds to run per load level")
	outDir := flag.String("outDir", "results", "directory to write JSON result files")
	flag.Parse()

	if *kubeContext == "" || *mode == "" || *strategyName == "" {
		log.Fatal("--context, --mode, and --strategy are required")
	}
	if *mode != "shared" && *mode != "webhook" {
		log.Fatal("--mode must be 'shared' or 'webhook'")
	}

	if err := os.MkdirAll(*outDir, 0755); err != nil {
		log.Fatalf("failed to create output dir: %v", err)
	}

	clientset, err := buildClientset(*kubeContext)
	if err != nil {
		log.Fatalf("failed to build clientset: %v", err)
	}

	levels := parseLoadLevels(*loadLevels)
	ctx := context.Background()

	for _, level := range levels {
		for seed := int64(1); seed <= int64(*seedCount); seed++ {
			fmt.Printf("\n=== %s | load=%d%% | seed=%d ===\n", *strategyName, level, seed)

			if err := cleanupPods(ctx, clientset); err != nil {
				log.Printf("cleanup error, skipping this run: %v", err)
				continue
			}

			result, err := runSingleTrial(ctx, clientset, *strategyName, *mode, level, seed)
			if err != nil {
				log.Printf("run failed: %v", err)
				continue
			}

			writeResult(*outDir, result)
		}
	}

	fmt.Println("\nAll runs complete.")
}

func parseLoadLevels(s string) []int {
	parts := strings.Split(s, ",")
	var levels []int
	for _, p := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			log.Fatalf("invalid load level %q: %v", p, err)
		}
		levels = append(levels, v)
	}
	return levels
}

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

func int64Ptr(i int64) *int64 {
	return &i
}

func cleanupPods(ctx context.Context, clientset *kubernetes.Clientset) error {
	gracePeriod := int64(0)
	if err := clientset.CoreV1().Pods("default").DeleteCollection(ctx,
		metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod},
		metav1.ListOptions{}); err != nil {
		return err
	}

	for i := 0; i < 30; i++ {
		pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		if len(pods.Items) == 0 {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("pods still present in default namespace after 30s wait -- cleanup did not complete in time")
}

func buildNodeToGPUID(ctx context.Context, clientset *kubernetes.Clientset) (map[string]string, error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: "gpu-id"})
	if err != nil {
		return nil, err
	}
	m := make(map[string]string)
	for _, n := range nodes.Items {
		if id, ok := n.Labels["gpu-id"]; ok {
			m[n.Name] = "GPU-" + id
		}
	}
	return m, nil
}

func runSingleTrial(ctx context.Context, clientset *kubernetes.Clientset, strategy, mode string, loadPct int, seed int64) (*RunResult, error) {
	nodeToGPUID, err := buildNodeToGPUID(ctx, clientset)
	if err != nil {
		return nil, fmt.Errorf("failed to build node->gpu-id map: %w", err)
	}

	targetDemandUnits := int(float64(totalComputeCapacity) * float64(loadPct) / 100.0)
	jobs := workload.GenerateBatchForDemand(targetDemandUnits, seed)
	n := len(jobs)

	totalUnits := 0
	for _, j := range jobs {
		totalUnits += j.Units
	}

	fmt.Printf("  Generated %d jobs (target load %d%%, total compute demand %d/%d units):\n", n, loadPct, totalUnits, totalComputeCapacity)
	fmt.Println("  NAME              UNITS  MEM(GB)  TYPE               DURATION(s)")
	for _, j := range jobs {
		fmt.Printf("  %-16s  %-5d  %-7d  %-17s  %d\n", j.Name, j.Units, j.MemoryGB, j.Type, j.ExpectedDurationSeconds)
	}

	// Sequential submission with wait-for-Running before the next job:
	// FetchClusterState only counts pods with status.phase=Running, so
	// submitting rapid-fire causes a real TOCTOU race -- a just-admitted
	// pod hasn't reached Running yet, so the next job's placement
	// decision is made against stale (too-optimistic) free-capacity
	// data. Waiting for Running specifically (not "Running or Pending",
	// since a freshly created pod starts in Pending immediately and that
	// condition was a no-op) fixes this for Phase 4 measurement purposes.
	// The race itself is investigated and properly fixed via real
	// reservation, not submission delay, in Phase 5.
	submissionErrors := 0
	deniedByWebhook := 0
	for _, j := range jobs {
		var err error
		if mode == "shared" {
			err = submitSharedResourceJob(ctx, clientset, j)
		} else {
			err = submitWebhookJob(ctx, clientset, j)
		}
		if err != nil {
			if strings.Contains(err.Error(), "admission webhook") && strings.Contains(err.Error(), "denied the request") {
				deniedByWebhook++
				log.Printf("webhook correctly denied %s (no fit): %v", j.Name, err)
			} else {
				log.Printf("failed to submit %s: %v", j.Name, err)
				submissionErrors++
			}
			continue
		}

		for i := 0; i < 10; i++ {
			pod, getErr := clientset.CoreV1().Pods("default").Get(ctx, j.Name, metav1.GetOptions{})
			if getErr == nil && pod.Status.Phase == corev1.PodRunning {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	time.Sleep(15 * time.Second)

	scheduled, pending, totalScheduledUnits, fetchErrors := 0, 0, 0, 0
	fmt.Println("  Outcomes:")
	fmt.Println("  NAME              STATUS      GPU")
	for _, j := range jobs {
		pod, err := clientset.CoreV1().Pods("default").Get(ctx, j.Name, metav1.GetOptions{})
		if err != nil {
			fmt.Printf("  %-16s  (denied or not found)\n", j.Name)
			fetchErrors++
			continue
		}
		gpuLabel := "(unscheduled)"
		if pod.Spec.NodeName != "" {
			if g, ok := nodeToGPUID[pod.Spec.NodeName]; ok {
				gpuLabel = g
			} else {
				gpuLabel = pod.Spec.NodeName
			}
		}
		fmt.Printf("  %-16s  %-10s  %s\n", j.Name, pod.Status.Phase, gpuLabel)
		if pod.Status.Phase == corev1.PodRunning {
			scheduled++
			totalScheduledUnits += j.Units
		} else if pod.Status.Phase == corev1.PodPending {
			pending++
		}
	}

	maxFree, totalFree, utilization, idleWaste, costPerJob, err := measureState(ctx, clientset, mode, scheduled)
	if err != nil {
		return nil, err
	}

	probeSucceeded := attemptProbeJob(ctx, clientset, mode, totalFree)

	valid := true
	notes := ""
	if submissionErrors > 0 {
		valid = false
		notes += fmt.Sprintf("%d submission errors; ", submissionErrors)
	}
	if fetchErrors > deniedByWebhook {
		valid = false
		notes += fmt.Sprintf("%d pods could not be fetched after settling (beyond the %d correctly denied by webhook); ", fetchErrors-deniedByWebhook, deniedByWebhook)
	}
	if scheduled+pending+fetchErrors != n {
		valid = false
		notes += "outcome count mismatch with generated job count; "
	}

	fmt.Printf("\n  SUMMARY: scheduled=%d/%d (%d/%d units)  denied=%d  maxFreeCompute=%d/10  totalFree=%d/%d (%.0f%%)  util=%.1f%%  idleWaste=$%.2f  costPerJob=$%.3f  probeSucceeded=%v  valid=%v\n",
		scheduled, n, totalScheduledUnits, totalUnits,
		deniedByWebhook,
		maxFree,
		totalFree, totalComputeCapacity, 100*float64(totalFree)/float64(totalComputeCapacity),
		utilization, idleWaste, costPerJob, probeSucceeded, valid)
	if !valid {
		fmt.Printf("  WARNING: run marked invalid -- %s\n", notes)
	}

	return &RunResult{
		Strategy:            strategy,
		Mode:                mode,
		LoadLevelPct:        loadPct,
		Seed:                seed,
		Timestamp:           time.Now(),
		TotalJobs:           n,
		TotalDemandUnits:    totalUnits,
		Scheduled:           scheduled,
		Pending:             pending,
		DeniedByWebhook:     deniedByWebhook,
		TotalScheduledUnits: totalScheduledUnits,
		SubmissionErrors:    submissionErrors,
		MaxFreeCompute:      maxFree,
		TotalFreeCompute:    totalFree,
		UtilizationPct:      utilization,
		IdleDollarsWasted:   idleWaste,
		CostPerJob:          costPerJob,
		ProbeJobSucceeded:   probeSucceeded,
		Valid:               valid,
		Notes:               notes,
	}, nil
}

func submitSharedResourceJob(ctx context.Context, clientset *kubernetes.Clientset, j workload.Job) error {
	computeQty := resourceapi.MustParse(strconv.Itoa(j.Units))
	memQty := resourceapi.MustParse(strconv.Itoa(j.MemoryGB))

	spec := corev1.PodSpec{
		TerminationGracePeriodSeconds: int64Ptr(0),
		Containers: []corev1.Container{
			{
				Name:    "workload",
				Image:   "busybox",
				Command: []string{"sleep", "3600"},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						"simulated.com/gpu-capacity":     computeQty,
						"simulated.com/gpu-capacity-mem": memQty,
					},
					Limits: corev1.ResourceList{
						"simulated.com/gpu-capacity":     computeQty,
						"simulated.com/gpu-capacity-mem": memQty,
					},
				},
			},
		},
	}

	// Mirror the webhook's hard tier-eligibility rule as a native K8s
	// nodeAffinity, so LeastAllocated/MostAllocated baselines respect the
	// same SLA-style constraint (latency-sensitive jobs excluded from
	// economy tier) rather than being compared against an easier,
	// unconstrained problem.
	if j.Type == "latency-sensitive" {
		spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "gpu-tier",
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{"premium", "mid"},
								},
							},
						},
					},
				},
			},
		}
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: j.Name},
		Spec:       spec,
	}
	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func submitWebhookJob(ctx context.Context, clientset *kubernetes.Clientset, j workload.Job) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: j.Name,
			Annotations: map[string]string{
				"gpu-units-needed":      strconv.Itoa(j.Units),
				"gpu-memory-needed":     strconv.Itoa(j.MemoryGB),
				"gpu-job-type":          j.Type,
				"gpu-expected-duration": strconv.Itoa(j.ExpectedDurationSeconds),
			},
		},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: int64Ptr(0),
			Containers: []corev1.Container{
				{Name: "workload", Image: "busybox", Command: []string{"sleep", "3600"}},
			},
		},
	}
	_, err := clientset.CoreV1().Pods("default").Create(ctx, pod, metav1.CreateOptions{})
	return err
}

func measureState(ctx context.Context, clientset *kubernetes.Clientset, mode string, scheduledCount int) (maxFree, totalFree int, utilizationPct, idleWaste, costPerJob float64, err error) {
	nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{LabelSelector: "gpu-id"})
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("failed to list nodes: %w", err)
	}

	pods, err := clientset.CoreV1().Pods("default").List(ctx, metav1.ListOptions{FieldSelector: "status.phase=Running"})
	if err != nil {
		return 0, 0, 0, 0, 0, fmt.Errorf("failed to list pods: %w", err)
	}

	usedPerNode := make(map[string]int)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue
		}
		for _, c := range pod.Spec.Containers {
			for resName, qty := range c.Resources.Requests {
				name := string(resName)
				if mode == "shared" && name == "simulated.com/gpu-capacity" {
					usedPerNode[pod.Spec.NodeName] += int(qty.Value())
				}
				if mode == "webhook" && strings.HasPrefix(name, "simulated.com/gpu-") && name != "simulated.com/gpu-capacity" {
					usedPerNode[pod.Spec.NodeName] += int(qty.Value())
				}
			}
		}
	}

	totalCapacity := 0
	totalUsed := 0
	totalCostPerHour := 0.0
	tierCosts := map[string]float64{"premium": premiumCost, "mid": midCost, "economy": economyCost}

	for _, node := range nodes.Items {
		used := usedPerNode[node.Name]
		free := computeCapacityEach - used
		totalFree += free
		if free > maxFree {
			maxFree = free
		}
		totalCapacity += computeCapacityEach
		totalUsed += used

		tier := node.Labels["gpu-tier"]
		totalCostPerHour += float64(used) * tierCosts[tier]
	}

	if totalCapacity > 0 {
		utilizationPct = 100.0 * float64(totalUsed) / float64(totalCapacity)
	}
	idleWaste = float64(totalFree) * avgTierCost(tierCosts)
	if scheduledCount > 0 {
		costPerJob = totalCostPerHour / float64(scheduledCount)
	}

	return maxFree, totalFree, utilizationPct, idleWaste, costPerJob, nil
}

func avgTierCost(tierCosts map[string]float64) float64 {
	sum := 0.0
	for _, c := range tierCosts {
		sum += c
	}
	return sum / float64(len(tierCosts))
}

func attemptProbeJob(ctx context.Context, clientset *kubernetes.Clientset, mode string, totalFree int) bool {
	if totalFree <= 0 {
		return false
	}
	probeUnits := totalFree
	if probeUnits > computeCapacityEach {
		probeUnits = computeCapacityEach
	}

	probeName := "probe-job"
	probeJob := workload.Job{Name: probeName, Units: probeUnits, MemoryGB: 1, Type: "batch-tolerant"}

	var err error
	if mode == "shared" {
		err = submitSharedResourceJob(ctx, clientset, probeJob)
	} else {
		err = submitWebhookJob(ctx, clientset, probeJob)
	}
	if err != nil {
		return false
	}

	time.Sleep(10 * time.Second)
	pod, err := clientset.CoreV1().Pods("default").Get(ctx, probeName, metav1.GetOptions{})
	succeeded := err == nil && pod.Status.Phase == corev1.PodRunning

	gracePeriod := int64(0)
	clientset.CoreV1().Pods("default").Delete(ctx, probeName, metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})

	for i := 0; i < 15; i++ {
		_, err := clientset.CoreV1().Pods("default").Get(ctx, probeName, metav1.GetOptions{})
		if err != nil {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return succeeded
}

func writeResult(outDir string, r *RunResult) {
	filename := fmt.Sprintf("%s_load%d_seed%d_%d.json", r.Strategy, r.LoadLevelPct, r.Seed, r.Timestamp.Unix())
	path := filepath.Join(outDir, filename)
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		log.Printf("failed to marshal result: %v", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		log.Printf("failed to write result file %s: %v", path, err)
	}
}

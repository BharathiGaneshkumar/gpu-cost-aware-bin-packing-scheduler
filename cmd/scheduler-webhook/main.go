package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"sync"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gpu-bin-packing-scheduler/internal/clusterstate"
	"gpu-bin-packing-scheduler/internal/scheduler"
)

const (
	unitsAnnotationKey    = "gpu-units-needed"
	memoryAnnotationKey   = "gpu-memory-needed"
	typeAnnotationKey     = "gpu-job-type"
	durationAnnotationKey = "gpu-expected-duration"

	memoryAllocatedAnnotationKey = "gpu-memory-allocated"
	allocatedIDAnnotationKey     = "gpu-allocated-id"

	rollingWindowSize = 20
)

var clientset *kubernetes.Clientset

// recentJobSizes is a simple in-memory rolling window of recent job unit
// sizes, used by the reservation-headroom heuristic. NOTE: this resets on
// pod restart and isn't shared across multiple webhook replicas -- a
// known, documented limitation, not a production-grade tracking mechanism.
var (
	recentJobSizes   []int
	recentJobSizesMu sync.Mutex
)
var reservationLedger = clusterstate.NewReservationLedger()

func main() {
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("failed to load in-cluster config: %v", err)
	}
	clientset, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to create clientset: %v", err)
	}

	http.HandleFunc("/mutate", handleMutate)
	log.Println("scheduler webhook listening on :8443 (TLS)")
	log.Fatal(http.ListenAndServeTLS(":8443", "/certs/tls.crt", "/certs/tls.key", nil))
}

func handleMutate(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var review admissionv1.AdmissionReview
	if err := json.Unmarshal(body, &review); err != nil {
		http.Error(w, "failed to parse AdmissionReview", http.StatusBadRequest)
		return
	}

	var pod corev1.Pod
	if err := json.Unmarshal(review.Request.Object.Raw, &pod); err != nil {
		http.Error(w, "failed to parse pod object", http.StatusBadRequest)
		return
	}

	log.Printf("received admission request for pod: %s", pod.Name)

	response := buildResponse(r.Context(), review.Request.UID, pod)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: response,
	})
}

func buildResponse(ctx context.Context, uid types.UID, pod corev1.Pod) *admissionv1.AdmissionResponse {
	base := &admissionv1.AdmissionResponse{UID: uid, Allowed: true}

	unitsStr, ok := pod.Annotations[unitsAnnotationKey]
	if !ok {
		return base // not a GPU job, allow through unchanged
	}
	units, err := strconv.Atoi(unitsStr)
	if err != nil || units <= 0 {
		log.Printf("pod %s has invalid %s annotation %q, allowing unmodified", pod.Name, unitsAnnotationKey, unitsStr)
		return base
	}

	memoryGB := parseIntAnnotation(pod, memoryAnnotationKey, 1) // default 1GB if unspecified
	jobType := parseJobType(pod)
	durationSeconds := parseIntAnnotation(pod, durationAnnotationKey, 0) // 0 = unspecified

	gpus, err := scheduler.FetchClusterState(ctx, clientset, reservationLedger)
	if err != nil {
		log.Printf("pod %s: failed to fetch cluster state: %v", pod.Name, err)
		base.Allowed = false
		base.Result = &metav1.Status{Message: "failed to fetch cluster state: " + err.Error()}
		return base
	}

	job := scheduler.Job{
		Units:                   units,
		MemoryGB:                memoryGB,
		Type:                    jobType,
		ExpectedDurationSeconds: durationSeconds,
	}

	recentAvg := recordAndGetRollingAverage(units)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	gpuID, err := scheduler.SelectGPU(gpus, job, recentAvg, rng)
	if err != nil {
		log.Printf("pod %s: no GPU fit for job (units=%d, memoryGB=%d, type=%s): %v", pod.Name, units, memoryGB, jobType, err)
		base.Allowed = false
		base.Result = &metav1.Status{Message: err.Error()}
		return base
	}

	log.Printf("pod %s: selected gpu-%s (units=%d, memoryGB=%d, type=%s, recentAvg=%d)", pod.Name, gpuID, units, memoryGB, jobType, recentAvg)

	reservationLedger.Add(gpuID, units, memoryGB, pod.Name)

	patch := buildPatch(pod, gpuID, units, memoryGB)
	patchBytes, _ := json.Marshal(patch)
	pt := admissionv1.PatchTypeJSONPatch
	base.Patch = patchBytes
	base.PatchType = &pt
	return base
}

// parseIntAnnotation reads an integer annotation, returning defaultVal if
// absent or invalid (invalid values are logged, not treated as errors --
// consistent with how unitsAnnotationKey's own invalid case is handled).
func parseIntAnnotation(pod corev1.Pod, key string, defaultVal int) int {
	val, ok := pod.Annotations[key]
	if !ok {
		return defaultVal
	}
	parsed, err := strconv.Atoi(val)
	if err != nil {
		log.Printf("pod %s has invalid %s annotation %q, using default %d", pod.Name, key, val, defaultVal)
		return defaultVal
	}
	return parsed
}

func parseJobType(pod corev1.Pod) scheduler.JobType {
	val, ok := pod.Annotations[typeAnnotationKey]
	if !ok {
		return scheduler.JobBatchTolerant // default: open eligibility if unspecified
	}
	switch val {
	case string(scheduler.JobLatencySensitive):
		return scheduler.JobLatencySensitive
	case string(scheduler.JobBatchTolerant):
		return scheduler.JobBatchTolerant
	default:
		log.Printf("pod %s has unrecognized %s annotation %q, defaulting to batch-tolerant", pod.Name, typeAnnotationKey, val)
		return scheduler.JobBatchTolerant
	}
}

// recordAndGetRollingAverage appends units to the in-memory rolling
// window and returns the current average. Not persisted, not shared
// across replicas -- a documented simplification, not production-grade.
func recordAndGetRollingAverage(units int) int {
	recentJobSizesMu.Lock()
	defer recentJobSizesMu.Unlock()

	recentJobSizes = append(recentJobSizes, units)
	if len(recentJobSizes) > rollingWindowSize {
		recentJobSizes = recentJobSizes[len(recentJobSizes)-rollingWindowSize:]
	}

	sum := 0
	for _, u := range recentJobSizes {
		sum += u
	}
	if len(recentJobSizes) == 0 {
		return 0
	}
	return sum / len(recentJobSizes)
}

// buildPatch constructs a JSON Patch that: (1) sets the real, K8s-enforced
// compute resource request/limit for the chosen GPU, and (2) stamps two
// annotations recording the placement decision -- gpu-allocated-id (which
// GPU) and gpu-memory-allocated (how much memory, self-tracked since
// memory isn't a real K8s resource in this simulation). clusterstate.go
// reads these annotations back to reconstruct memory usage per node.
func buildPatch(pod corev1.Pod, gpuID string, units, memoryGB int) []map[string]interface{} {
	resourceName := "simulated.com/gpu-" + gpuID
	unitsStr := strconv.Itoa(units)

	patch := []map[string]interface{}{
		{
			"op":   "add",
			"path": "/spec/containers/0/resources",
			"value": map[string]interface{}{
				"requests": map[string]string{resourceName: unitsStr},
				"limits":   map[string]string{resourceName: unitsStr},
			},
		},
	}

	// pod.Annotations is guaranteed non-empty here (the pod already has
	// at least unitsAnnotationKey set, or buildResponse would have
	// returned early) -- so /metadata/annotations exists as a real path,
	// and "add" for individual keys is safe (unlike the empty-resources
	// case we hit earlier with the omitempty bug).
	patch = append(patch,
		map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations/" + allocatedIDAnnotationKey,
			"value": gpuID,
		},
		map[string]interface{}{
			"op":    "add",
			"path":  "/metadata/annotations/" + memoryAllocatedAnnotationKey,
			"value": strconv.Itoa(memoryGB),
		},
	)

	return patch
}

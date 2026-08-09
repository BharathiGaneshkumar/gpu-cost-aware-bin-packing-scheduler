package main

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gpu-bin-packing-scheduler/internal/scheduler"
)

const annotationKey = "gpu-units-needed"

// clientset is built once at startup and reused across requests.
var clientset *kubernetes.Clientset

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

	unitsStr, ok := pod.Annotations[annotationKey]
	if !ok {
		return base
	}
	units, err := strconv.Atoi(unitsStr)
	if err != nil || units <= 0 {
		log.Printf("pod %s has invalid %s annotation %q, allowing unmodified", pod.Name, annotationKey, unitsStr)
		return base
	}

	gpus, err := scheduler.FetchClusterState(ctx, clientset)
	if err != nil {
		log.Printf("pod %s: failed to fetch cluster state: %v", pod.Name, err)
		base.Allowed = false
		base.Result = &metav1.Status{Message: "failed to fetch cluster state: " + err.Error()}
		return base
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	job := scheduler.Job{
		Units:    units,
		MemoryGB: 1,                          // TEMPORARY placeholder -- Chunk 4 will parse a real gpu-memory-needed annotation
		Type:     scheduler.JobBatchTolerant, // TEMPORARY placeholder -- Chunk 4 will parse a real gpu-job-type annotation
	}
	gpuID, err := scheduler.SelectGPU(gpus, job, 0, rng) // TEMPORARY: recentAvgJobSize=0 disables headroom heuristic until real rolling-average tracking is wired in
	if err != nil {
		log.Printf("pod %s: no GPU fit for %d units: %v", pod.Name, units, err)
		base.Allowed = false
		base.Result = &metav1.Status{Message: err.Error()}
		return base
	}

	log.Printf("pod %s: selected gpu-%s for %d units", pod.Name, gpuID, units)

	patch := buildPatch(gpuID, units)
	patchBytes, _ := json.Marshal(patch)
	pt := admissionv1.PatchTypeJSONPatch
	base.Patch = patchBytes
	base.PatchType = &pt
	return base
}

func buildPatch(gpuID string, units int) []map[string]interface{} {
	resourceName := "simulated.com/gpu-" + gpuID
	unitsStr := strconv.Itoa(units)
	return []map[string]interface{}{
		{
			"op":   "add",
			"path": "/spec/containers/0/resources",
			"value": map[string]interface{}{
				"requests": map[string]string{resourceName: unitsStr},
				"limits":   map[string]string{resourceName: unitsStr},
			},
		},
	}
}

func jsonPatchEscape(s string) string {
	escaped := ""
	for _, c := range s {
		if c == '/' {
			escaped += "~1"
		} else if c == '~' {
			escaped += "~0"
		} else {
			escaped += string(c)
		}
	}
	return escaped
}

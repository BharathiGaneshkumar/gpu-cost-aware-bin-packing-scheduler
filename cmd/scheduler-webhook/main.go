package main

import (
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

	"gpu-bin-packing-scheduler/internal/scheduler"
)

// annotationKey is how a pod declares how many GPU units it needs,
// without naming a specific GPU -- our webhook decides that part.
const annotationKey = "gpu-units-needed"

func main() {
	http.HandleFunc("/mutate", handleMutate)
	log.Println("scheduler webhook listening on :8443")
	log.Fatal(http.ListenAndServe(":8443", nil))
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

	response := buildResponse(review.Request.UID, pod)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: response,
	})
}

// buildResponse decides whether this pod needs GPU placement, and if so,
// runs the scoring logic (against hardcoded fake cluster state for now --
// real cluster queries come in the next increment) and returns a patch
// adding the chosen GPU's resource request.
func buildResponse(uid types.UID, pod corev1.Pod) *admissionv1.AdmissionResponse {
	base := &admissionv1.AdmissionResponse{UID: uid, Allowed: true}

	unitsStr, ok := pod.Annotations[annotationKey]
	if !ok {
		// Not a GPU job -- allow through unchanged.
		return base
	}
	units, err := strconv.Atoi(unitsStr)
	if err != nil || units <= 0 {
		log.Printf("pod %s has invalid %s annotation %q, allowing unmodified", pod.Name, annotationKey, unitsStr)
		return base
	}

	// TEMPORARY hardcoded fake cluster state -- proves the SelectGPU call
	// path works before we wire in a real client-go cluster query next.
	fakeGPUs := []scheduler.GPU{
		{ID: "0", Capacity: 10, FreeUnits: 5, CostTier: 1, LastUsed: time.Now()},
		{ID: "1", Capacity: 10, FreeUnits: 3, CostTier: 1, LastUsed: time.Now()},
		{ID: "2", Capacity: 10, FreeUnits: 8, CostTier: 1, LastUsed: time.Now()},
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	gpuID, err := scheduler.SelectGPU(fakeGPUs, units, rng)
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

// buildPatch constructs a JSON Patch adding the chosen GPU's resource
// name to the pod's first container's requests and limits.
func buildPatch(gpuID string, units int) []map[string]interface{} {
	resourceName := "simulated.com/gpu-" + gpuID
	unitsStr := strconv.Itoa(units)
	return []map[string]interface{}{
		{
			"op":    "add",
			"path":  "/spec/containers/0/resources/requests/" + jsonPatchEscape(resourceName),
			"value": unitsStr,
		},
		{
			"op":    "add",
			"path":  "/spec/containers/0/resources/limits/" + jsonPatchEscape(resourceName),
			"value": unitsStr,
		},
	}
}

// jsonPatchEscape escapes "/" in a JSON Patch path segment, since our
// resource name itself contains a "/" (e.g. "simulated.com/gpu-0") which
// would otherwise be misread as a path separator.
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

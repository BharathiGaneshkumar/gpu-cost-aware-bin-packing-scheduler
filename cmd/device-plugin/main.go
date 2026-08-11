package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"gpu-bin-packing-scheduler/internal/deviceplugin"
)

var kubeletSocket = pluginapi.DevicePluginPath + "kubelet.sock"

// tierMemoryGB maps each tier to its simulated VRAM capacity, matching
// the same values used in internal/scheduler/clusterstate.go's tierSpecs.
var tierMemoryGB = map[string]int{
	"premium": 80,
	"mid":     40,
	"economy": 16,
}

func main() {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatal("NODE_NAME env var must be set via Downward API (spec.nodeName)")
	}

	gpuID, tier, err := lookupGPUIDAndTierFromNodeLabels(nodeName)
	if err != nil {
		log.Fatalf("failed to look up node labels for %s: %v", nodeName, err)
	}

	// Server 1: the existing per-GPU compute resource, used by our
	// webhook's bin-packing logic (simulated.com/gpu-0, gpu-1, etc.)
	perGPUResource := fmt.Sprintf("simulated.com/gpu-%s", gpuID)
	perGPUSocket := fmt.Sprintf("simulatedgpu-%s.sock", gpuID)
	if err := startPluginServer(perGPUSocket, perGPUResource, gpuID, 0); err != nil {
		log.Fatalf("failed to start per-GPU plugin server: %v", err)
	}

	// Server 2: a shared, identically-named COMPUTE resource across all
	// GPU nodes, so the real Kubernetes scheduler (LeastAllocated /
	// MostAllocated baselines) has multiple nodes to choose between.
	sharedComputeResource := "simulated.com/gpu-capacity"
	sharedComputeSocket := fmt.Sprintf("simulatedgpu-capacity-%s.sock", gpuID)
	if err := startPluginServer(sharedComputeSocket, sharedComputeResource, gpuID, 0); err != nil {
		log.Fatalf("failed to start shared-capacity plugin server: %v", err)
	}

	// Server 3: a shared MEMORY resource, sized per this node's tier
	// (16/40/80 GB), so the real K8s scheduler's baseline runs can be
	// made to respect the same 2D compute+memory constraint our webhook
	// enforces internally -- needed for a genuinely fair Phase 4
	// comparison, not just a compute-only one.
	memGB, ok := tierMemoryGB[tier]
	if !ok {
		log.Fatalf("unknown tier %q for node %s, cannot size memory resource", tier, nodeName)
	}
	sharedMemResource := "simulated.com/gpu-capacity-mem"
	sharedMemSocket := fmt.Sprintf("simulatedgpu-capacity-mem-%s.sock", gpuID)
	if err := startPluginServer(sharedMemSocket, sharedMemResource, gpuID, memGB); err != nil {
		log.Fatalf("failed to start shared-memory plugin server: %v", err)
	}

	log.Println("all three plugin servers running")
	select {}
}

// startPluginServer starts a gRPC device plugin server on its own socket,
// registers it with the kubelet under the given resource name, and
// returns once registration succeeds. deviceCount=0 defaults to 10
// (compute units); pass a tier's memory GB value for memory resources.
func startPluginServer(socketFile, resourceName, gpuID string, deviceCount int) error {
	socketPath := pluginapi.DevicePluginPath + socketFile

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove old socket %s: %w", socketPath, err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", socketPath, err)
	}

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, &deviceplugin.SimulatedGPUPlugin{GPUID: gpuID, DeviceCount: deviceCount})

	go func() {
		log.Printf("plugin server for resource %s listening on %s", resourceName, socketPath)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("grpc server for %s stopped: %v", resourceName, err)
		}
	}()

	time.Sleep(1 * time.Second)

	if err := registerWithKubelet(socketFile, resourceName); err != nil {
		return fmt.Errorf("failed to register %s: %w", resourceName, err)
	}
	log.Printf("registered resource %s with kubelet", resourceName)
	return nil
}

func registerWithKubelet(socketFile, resourceName string) error {
	conn, err := grpc.NewClient(
		"unix://"+kubeletSocket,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	defer conn.Close()

	client := pluginapi.NewRegistrationClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Register(ctx, &pluginapi.RegisterRequest{
		Version:      pluginapi.Version,
		Endpoint:     filepath.Base(socketFile),
		ResourceName: resourceName,
	})
	return err
}

func lookupGPUIDAndTierFromNodeLabels(nodeName string) (gpuID, tier string, err error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", "", fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", "", fmt.Errorf("failed to create clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	id, ok := node.Labels["gpu-id"]
	if !ok {
		return "", "", fmt.Errorf("node %s has no gpu-id label", nodeName)
	}
	t, ok := node.Labels["gpu-tier"]
	if !ok {
		return "", "", fmt.Errorf("node %s has no gpu-tier label", nodeName)
	}
	return id, t, nil
}

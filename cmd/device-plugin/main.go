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

var (
	gpuID         string
	resourceName  string
	socketPath    string
	kubeletSocket = pluginapi.DevicePluginPath + "kubelet.sock"
)

func main() {
	nodeName := os.Getenv("NODE_NAME")
	if nodeName == "" {
		log.Fatal("NODE_NAME env var must be set via Downward API (spec.nodeName)")
	}

	var err error
	gpuID, err = lookupGPUIDFromNodeLabel(nodeName)
	if err != nil {
		log.Fatalf("failed to look up gpu-id label for node %s: %v", nodeName, err)
	}

	resourceName = fmt.Sprintf("simulated.com/gpu-%s", gpuID)
	socketPath = pluginapi.DevicePluginPath + fmt.Sprintf("simulatedgpu-%s.sock", gpuID)

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("failed to remove old socket: %v", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", socketPath, err)
	}

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, &deviceplugin.SimulatedGPUPlugin{GPUID: gpuID})

	go func() {
		log.Printf("device plugin for GPU %s listening on %s", gpuID, socketPath)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("grpc server stopped: %v", err)
		}
	}()

	time.Sleep(1 * time.Second)

	if err := registerWithKubelet(); err != nil {
		log.Fatalf("failed to register with kubelet: %v", err)
	}

	log.Printf("registered resource %s with kubelet, plugin running", resourceName)
	select {}
}

// lookupGPUIDFromNodeLabel authenticates to the Kubernetes API using this
// pod's ServiceAccount (in-cluster config), fetches the Node object this
// pod is running on, and returns its "gpu-id" label value.
func lookupGPUIDFromNodeLabel(nodeName string) (string, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return "", fmt.Errorf("failed to create clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	node, err := clientset.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to get node %s: %w", nodeName, err)
	}

	id, ok := node.Labels["gpu-id"]
	if !ok {
		return "", fmt.Errorf("node %s has no gpu-id label", nodeName)
	}
	return id, nil
}

func registerWithKubelet() error {
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
		Endpoint:     filepath.Base(socketPath),
		ResourceName: resourceName,
	})
	return err
}

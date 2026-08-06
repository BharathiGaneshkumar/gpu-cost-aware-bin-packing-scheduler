package main

import (
	"context"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"

	"gpu-bin-packing-scheduler/internal/deviceplugin"
)

const (
	// resourceName is what pods will request, e.g. resources.limits["simulated.com/gpu"]: 1
	resourceName = "simulated.com/gpu"
	// socketPath is where the kubelet expects to find our plugin's socket
	socketPath    = pluginapi.DevicePluginPath + "simulatedgpu.sock"
	kubeletSocket = pluginapi.DevicePluginPath + "kubelet.sock"
)

func main() {
	// Clean up any stale socket from a previous run.
	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		log.Fatalf("failed to remove old socket: %v", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		log.Fatalf("failed to listen on %s: %v", socketPath, err)
	}

	grpcServer := grpc.NewServer()
	pluginapi.RegisterDevicePluginServer(grpcServer, &deviceplugin.SimulatedGPUPlugin{})

	go func() {
		log.Printf("device plugin gRPC server listening on %s", socketPath)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("grpc server stopped: %v", err)
		}
	}()

	// Give the server a moment to actually start accepting connections
	// before we try to register with the kubelet.
	time.Sleep(1 * time.Second)

	if err := registerWithKubelet(); err != nil {
		log.Fatalf("failed to register with kubelet: %v", err)
	}

	log.Println("registered with kubelet, plugin running")
	select {} // block forever
}

// registerWithKubelet calls the kubelet's well-known registration socket
// once at startup to announce this plugin's socket path and resource name.
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

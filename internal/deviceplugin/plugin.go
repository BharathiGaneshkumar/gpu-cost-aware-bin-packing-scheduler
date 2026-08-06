package deviceplugin

import (
	"context"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

// SimulatedGPUPlugin is our stub implementation of the Kubernetes device plugin
// interface. It doesn't talk to any real hardware yet -- every method just
// returns the minimum valid response so the gRPC server compiles and can
// register with the kubelet.
type SimulatedGPUPlugin struct {
	pluginapi.UnimplementedDevicePluginServer
}

// GetDevicePluginOptions tells the kubelet what optional features this
// plugin supports. We support none of them yet, so every flag is false.
func (p *SimulatedGPUPlugin) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// ListAndWatch is the streaming call where we'd normally report our fake
// GPU list. For now it does nothing -- we'll fill this in next increment.
func (p *SimulatedGPUPlugin) ListAndWatch(e *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	<-stream.Context().Done()
	return nil
}

// Allocate is called when the kubelet wants to hand a device to a
// container. Stub for now -- returns an empty response per requested pod.
func (p *SimulatedGPUPlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := &pluginapi.AllocateResponse{}
	for range reqs.ContainerRequests {
		resp.ContainerResponses = append(resp.ContainerResponses, &pluginapi.ContainerAllocateResponse{})
	}
	return resp, nil
}

// PreStartContainer is an optional hook we don't need. Stub only.
func (p *SimulatedGPUPlugin) PreStartContainer(ctx context.Context, r *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

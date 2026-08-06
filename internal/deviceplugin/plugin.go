package deviceplugin

import (
	"context"
	"fmt"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	numGPUs     = 4
	unitsPerGPU = 10
)

// SimulatedGPUPlugin is our stub implementation of the Kubernetes device
// plugin interface. It doesn't talk to any real hardware -- it reports a
// fixed pool of simulated fractional GPU "units" so we can build and test
// scheduling logic without needing real MIG-capable hardware.
type SimulatedGPUPlugin struct {
	pluginapi.UnimplementedDevicePluginServer
}

// GetDevicePluginOptions tells the kubelet what optional features this
// plugin supports. We support none of them yet, so every flag is false.
func (p *SimulatedGPUPlugin) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// buildDeviceList constructs our simulated pool: numGPUs GPUs, each split
// into unitsPerGPU fractional units. Device IDs encode which GPU each unit
// belongs to (e.g. "gpu-0-unit-3") so the scheduler can later group units
// back to their physical GPU.
func buildDeviceList() []*pluginapi.Device {
	var devices []*pluginapi.Device
	for g := 0; g < numGPUs; g++ {
		for u := 0; u < unitsPerGPU; u++ {
			devices = append(devices, &pluginapi.Device{
				ID:     fmt.Sprintf("gpu-%d-unit-%d", g, u),
				Health: pluginapi.Healthy,
			})
		}
	}
	return devices
}

// ListAndWatch reports our simulated device list once, then blocks,
// keeping the stream open as the protocol expects. We don't simulate
// devices going unhealthy or changing over time yet -- fixed list only.
func (p *SimulatedGPUPlugin) ListAndWatch(e *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	resp := &pluginapi.ListAndWatchResponse{Devices: buildDeviceList()}
	if err := stream.Send(resp); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

// Allocate is called when the kubelet wants to hand devices to a
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

package deviceplugin

import (
	"context"
	"fmt"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const defaultUnitsPerGPU = 10

// SimulatedGPUPlugin reports a simulated pool of fractional units for ONE
// physical GPU (this node represents a single GPU in our simulated
// topology). GPUID identifies which GPU this instance represents.
// DeviceCount controls how many devices to report -- defaults to 10
// (compute units) if left zero, but can be set to a tier's memory GB
// value (16/40/80) when this plugin instance is registering a memory
// resource instead of a compute one.
type SimulatedGPUPlugin struct {
	pluginapi.UnimplementedDevicePluginServer
	GPUID       string
	DeviceCount int
}

func (p *SimulatedGPUPlugin) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

func (p *SimulatedGPUPlugin) buildDeviceList() []*pluginapi.Device {
	count := p.DeviceCount
	if count == 0 {
		count = defaultUnitsPerGPU
	}
	var devices []*pluginapi.Device
	for u := 0; u < count; u++ {
		devices = append(devices, &pluginapi.Device{
			ID:     fmt.Sprintf("gpu-%s-unit-%d", p.GPUID, u),
			Health: pluginapi.Healthy,
		})
	}
	return devices
}

func (p *SimulatedGPUPlugin) ListAndWatch(e *pluginapi.Empty, stream pluginapi.DevicePlugin_ListAndWatchServer) error {
	resp := &pluginapi.ListAndWatchResponse{Devices: p.buildDeviceList()}
	if err := stream.Send(resp); err != nil {
		return err
	}
	<-stream.Context().Done()
	return nil
}

func (p *SimulatedGPUPlugin) Allocate(ctx context.Context, reqs *pluginapi.AllocateRequest) (*pluginapi.AllocateResponse, error) {
	resp := &pluginapi.AllocateResponse{}
	for range reqs.ContainerRequests {
		resp.ContainerResponses = append(resp.ContainerResponses, &pluginapi.ContainerAllocateResponse{})
	}
	return resp, nil
}

func (p *SimulatedGPUPlugin) PreStartContainer(ctx context.Context, r *pluginapi.PreStartContainerRequest) (*pluginapi.PreStartContainerResponse, error) {
	return &pluginapi.PreStartContainerResponse{}, nil
}

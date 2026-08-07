package deviceplugin

import (
	"context"
	"fmt"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const unitsPerGPU = 10

// SimulatedGPUPlugin reports a simulated pool of fractional units for ONE
// physical GPU (this node represents a single GPU in our simulated
// topology). GPUID identifies which GPU this instance represents, purely
// for device ID/logging purposes -- the resource name itself (set in
// main.go, e.g. "simulated.com/gpu-2") is what Kubernetes actually uses
// to distinguish GPUs from each other.
type SimulatedGPUPlugin struct {
	pluginapi.UnimplementedDevicePluginServer
	GPUID string
}

func (p *SimulatedGPUPlugin) GetDevicePluginOptions(ctx context.Context, e *pluginapi.Empty) (*pluginapi.DevicePluginOptions, error) {
	return &pluginapi.DevicePluginOptions{}, nil
}

// buildDeviceList returns unitsPerGPU devices, all belonging to this one
// GPU. IDs no longer need to encode a GPU number for grouping purposes --
// the resource name this plugin registers under already scopes these
// units to one GPU -- but we keep GPUID in the ID string for readability
// when inspecting checkpoint/logs.
func (p *SimulatedGPUPlugin) buildDeviceList() []*pluginapi.Device {
	var devices []*pluginapi.Device
	for u := 0; u < unitsPerGPU; u++ {
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

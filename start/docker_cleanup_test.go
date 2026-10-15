package start

import (
	"context"
	"errors"

	sdkclient "github.com/docker/go-sdk/client"
	"github.com/flanksource/deps/start/state"
	mobyclient "github.com/moby/moby/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type stopClient struct {
	sdkclient.SDKClient
	calls     []string
	removeErr error
}

func (c *stopClient) ContainerStop(_ context.Context, id string, _ mobyclient.ContainerStopOptions) (mobyclient.ContainerStopResult, error) {
	c.calls = append(c.calls, "stop "+id)
	return mobyclient.ContainerStopResult{}, nil
}

func (c *stopClient) ContainerRemove(_ context.Context, id string, _ mobyclient.ContainerRemoveOptions) (mobyclient.ContainerRemoveResult, error) {
	c.calls = append(c.calls, "remove "+id)
	return mobyclient.ContainerRemoveResult{}, c.removeErr
}

var _ = Describe("docker isolated cleanup", func() {
	It("removes an isolated container after stopping it", func() {
		client := &stopClient{}
		st := &state.State{ContainerID: "container-a", StartOptions: &state.StartOptions{InstanceID: "fixture-a"}}
		Expect((&dockerRuntime{docker: client}).Stop(context.Background(), st)).To(Succeed())
		Expect(client.calls).To(Equal([]string{"stop container-a", "remove container-a"}))
	})

	It("preserves the reusable default container", func() {
		client := &stopClient{}
		st := &state.State{ContainerID: "container-a"}
		Expect((&dockerRuntime{docker: client}).Stop(context.Background(), st)).To(Succeed())
		Expect(client.calls).To(Equal([]string{"stop container-a"}))
	})

	It("reports a failed isolated-container removal", func() {
		client := &stopClient{removeErr: errors.New("remove failed")}
		st := &state.State{ContainerID: "container-a", StartOptions: &state.StartOptions{InstanceID: "fixture-a"}}
		Expect((&dockerRuntime{docker: client}).Stop(context.Background(), st)).To(MatchError(ContainSubstring("remove failed")))
	})
})

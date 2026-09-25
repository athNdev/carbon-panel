package nodeagent

import (
	"context"
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/cloud/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockRunner_Lifecycle(t *testing.T) {
	ctx := context.Background()
	runner := NewMockRunner()

	w := &v1.Workload{
		Id:       "wl-test-1",
		Name:     "Survival",
		OrgId:    "org-1",
		HostPort: 25565,
		Spec: &v1.WorkloadSpec{
			Loader:           "PAPER",
			MinecraftVersion: "1.21.4",
			MemoryMb:         2048,
		},
	}

	cid, port, err := runner.Assign(ctx, w, true)
	require.NoError(t, err)
	assert.Equal(t, "mock-container-wl-test-1", cid)
	assert.Equal(t, int32(25565), port)
	assert.Equal(t, cid, runner.Containers["wl-test-1"])

	err = runner.Stop(ctx, "wl-test-1", 10)
	require.NoError(t, err)
	assert.Empty(t, runner.Containers["wl-test-1"])

	err = runner.Delete(ctx, "wl-test-1", true)
	require.NoError(t, err)
}

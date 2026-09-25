package blueprint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuiltinBlueprints(t *testing.T) {
	bps := BuiltinBlueprints()
	require.NotEmpty(t, bps)

	ids := make(map[string]bool)
	for _, bp := range bps {
		require.NotEmpty(t, bp.Id)
		require.NotEmpty(t, bp.Name)
		require.NotEmpty(t, bp.Loader)
		require.NotEmpty(t, bp.DockerImage)
		require.True(t, bp.Builtin)
		require.Greater(t, bp.DefaultMemoryMb, int64(0))
		require.Greater(t, bp.DefaultCpuMillicores, int64(0))
		require.False(t, ids[bp.Id], "duplicate builtin ID: %s", bp.Id)
		ids[bp.Id] = true
	}

	// Lookup test
	paper := FindBuiltin("paper")
	require.NotNil(t, paper)
	require.Equal(t, "PaperMC", paper.Name)
	require.Equal(t, int64(4096), paper.DefaultMemoryMb)

	bedrock := FindBuiltin("bedrock")
	require.NotNil(t, bedrock)
	require.Equal(t, "itzg/minecraft-bedrock-server:latest", bedrock.DockerImage)

	unknown := FindBuiltin("nonexistent-flavor")
	require.Nil(t, unknown)
}

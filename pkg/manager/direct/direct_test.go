package direct

import (
	"context"
	"testing"

	"github.com/flanksource/deps/pkg/platform"
	"github.com/flanksource/deps/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVersionConstraint(t *testing.T) {
	mgr := NewDirectURLManager()
	pkg := types.Package{Name: "op"}
	plat := platform.Platform{OS: "linux", Arch: "amd64"}

	tests := []struct {
		name       string
		constraint string
		want       string
		wantErr    bool
	}{
		{name: "exact version", constraint: "2.31.1", want: "2.31.1"},
		{name: "exact version with v prefix", constraint: "v2.31.1", want: "2.31.1"},
		{name: "latest alias rejected", constraint: "latest", wantErr: true},
		{name: "stable alias rejected", constraint: "stable", wantErr: true},
		{name: "semver range rejected", constraint: "^2.31.0", wantErr: true},
		{name: "partial version rejected", constraint: "2.31", wantErr: true},
		{name: "empty rejected", constraint: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := mgr.ResolveVersionConstraint(context.Background(), pkg, tt.constraint, plat)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "requires an explicit version")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

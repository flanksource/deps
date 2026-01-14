package github

import (
	"context"
	"testing"
)

func TestDiscoverVersionsViaGit(t *testing.T) {
	tests := []struct {
		name     string
		owner    string
		repo     string
		minTags  int
		wantErr  bool
	}{
		{
			name:    "flux2",
			owner:   "fluxcd",
			repo:    "flux2",
			minTags: 20,
			wantErr: false,
		},
		{
			name:    "helm",
			owner:   "helm",
			repo:    "helm",
			minTags: 50,
			wantErr: false,
		},
		{
			name:    "kubernetes",
			owner:   "kubernetes",
			repo:    "kubernetes",
			minTags: 100,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versions, err := DiscoverVersionsViaGit(context.Background(), tt.owner, tt.repo)
			if (err != nil) != tt.wantErr {
				t.Errorf("DiscoverVersionsViaGit() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(versions) < tt.minTags {
				t.Errorf("DiscoverVersionsViaGit() got %d versions, want at least %d", len(versions), tt.minTags)
			}
			t.Logf("Found %d versions for %s/%s", len(versions), tt.owner, tt.repo)
			if len(versions) > 0 {
				t.Logf("First 5 versions: %v", versions[:min(5, len(versions))])
			}
		})
	}
}

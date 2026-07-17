// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"

	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type fakeHistoryClient struct {
	releases []*v1.Release
	err      error
}

func (f *fakeHistoryClient) GetReleaseHistory(context.Context, string, string) ([]*v1.Release, error) {
	return f.releases, f.err
}

func releaseAt(version int, status common.Status) *v1.Release {
	return &v1.Release{
		Name:     "web-production",
		Version:  version,
		Manifest: fmt.Sprintf("manifest-v%d", version),
		Info:     &v1.Info{Status: status},
	}
}

// TestLastSuccessfulRelease walks release history newest-first and returns
// the newest deployed/superseded revision, with warnings when the latest
// revision itself is not successful.
func TestLastSuccessfulRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		releases        []*v1.Release
		err             error
		wantErr         bool
		wantNilRelease  bool
		wantVersion     int
		wantWarning     bool
		warningContains string
	}{
		{
			name:           "ErrReleaseNotFound is fresh install",
			err:            helm.ErrReleaseNotFound,
			wantNilRelease: true,
		},
		{
			name:    "other history errors propagate",
			err:     errors.New("boom"),
			wantErr: true,
		},
		{
			name: "latest deployed is used directly",
			releases: []*v1.Release{
				releaseAt(1, common.StatusSuperseded),
				releaseAt(2, common.StatusDeployed),
			},
			wantVersion: 2,
		},
		{
			name: "unsorted history is sorted newest first",
			releases: []*v1.Release{
				releaseAt(2, common.StatusDeployed),
				releaseAt(3, common.StatusSuperseded),
				releaseAt(1, common.StatusSuperseded),
			},
			wantVersion: 3,
		},
		{
			name: "failed latest falls back with warning",
			releases: []*v1.Release{
				releaseAt(1, common.StatusDeployed),
				releaseAt(2, common.StatusFailed),
			},
			wantVersion:     1,
			wantWarning:     true,
			warningContains: "revision 2",
		},
		{
			name: "pending latest falls back with warning",
			releases: []*v1.Release{
				releaseAt(1, common.StatusDeployed),
				releaseAt(2, common.StatusPendingUpgrade),
			},
			wantVersion: 1,
			wantWarning: true,
		},
		{
			name: "no revision ever succeeded",
			releases: []*v1.Release{
				releaseAt(1, common.StatusFailed),
				releaseAt(2, common.StatusFailed),
			},
			wantNilRelease: true,
			wantWarning:    true,
		},
		{
			name:           "empty history is fresh install",
			releases:       []*v1.Release{},
			wantNilRelease: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := &fakeHistoryClient{releases: tt.releases, err: tt.err}

			rel, warning, err := LastSuccessfulRelease(t.Context(), client, "web", "production")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNilRelease {
				assert.Nil(t, rel)
			} else {
				require.NotNil(t, rel)
				assert.Equal(t, tt.wantVersion, rel.Version)
			}
			if tt.wantWarning {
				assert.NotEmpty(t, warning)
				if tt.warningContains != "" {
					assert.Contains(t, warning, tt.warningContains)
				}
			} else {
				assert.Empty(t, warning)
			}
		})
	}
}

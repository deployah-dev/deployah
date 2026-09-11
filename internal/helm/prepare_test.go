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

package helm

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/release/common"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/runtime/schema"

	v1 "helm.sh/helm/v4/pkg/release/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

const (
	applySSA = string(v1.ApplyMethodServerSideApply)
	applyCSA = string(v1.ApplyMethodClientSideApply)
)

func prepRelease(version int, status common.Status, applyMethod string) *v1.Release {
	return &v1.Release{
		Name:        "web-production",
		Version:     version,
		ApplyMethod: applyMethod,
		Info:        &v1.Info{Status: status},
	}
}

func TestPrepareRelease(t *testing.T) {
	t.Parallel()

	deployed := prepRelease(4, common.StatusDeployed, applySSA)
	failedSSA := prepRelease(5, common.StatusFailed, applySSA)
	failedCSA := prepRelease(5, common.StatusFailed, applyCSA)
	supersededSSA := prepRelease(5, common.StatusSuperseded, applySSA)
	supersededEmpty := prepRelease(5, common.StatusSuperseded, "")
	olderDeployedSSA := prepRelease(3, common.StatusDeployed, applySSA)
	olderDeployedCSA := prepRelease(4, common.StatusDeployed, applyCSA)

	tests := []struct {
		name    string
		history []*v1.Release
		want    ReleasePrep
		wantErr error
	}{
		{
			name:    "nil history is install",
			history: nil,
			want: ReleasePrep{
				Operation:    OperationInstall,
				NextRevision: 1,
			},
		},
		{
			name:    "empty history is install",
			history: []*v1.Release{},
			want: ReleasePrep{
				Operation:    OperationInstall,
				NextRevision: 1,
			},
		},
		{
			name:    "latest deployed ssa is upgrade",
			history: []*v1.Release{prepRelease(1, common.StatusSuperseded, applySSA), deployed},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       deployed,
				Current:      deployed,
				NextRevision: 5,
			},
		},
		{
			name:    "unsorted history still picks highest version",
			history: []*v1.Release{deployed, prepRelease(1, common.StatusSuperseded, applySSA), prepRelease(2, common.StatusSuperseded, applySSA)},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       deployed,
				Current:      deployed,
				NextRevision: 5,
			},
		},
		{
			name:    "pending-install latest",
			history: []*v1.Release{prepRelease(1, common.StatusPendingInstall, "")},
			wantErr: ErrReleasePending,
		},
		{
			name:    "pending-upgrade latest",
			history: []*v1.Release{olderDeployedSSA, prepRelease(4, common.StatusPendingUpgrade, applyCSA)},
			wantErr: ErrReleasePending,
		},
		{
			name:    "pending-rollback latest",
			history: []*v1.Release{olderDeployedSSA, prepRelease(4, common.StatusPendingRollback, applySSA)},
			wantErr: ErrReleasePending,
		},
		{
			name:    "failed newest ssa with older deployed ssa",
			history: []*v1.Release{olderDeployedSSA, failedSSA},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failedSSA,
				Current:      olderDeployedSSA,
				NextRevision: 6,
			},
		},
		{
			name:    "superseded newest ssa with older deployed ssa",
			history: []*v1.Release{olderDeployedSSA, supersededSSA},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       supersededSSA,
				Current:      olderDeployedSSA,
				NextRevision: 6,
			},
		},
		{
			name:    "failed-only ssa is upgrade not install",
			history: []*v1.Release{failedSSA},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failedSSA,
				Current:      failedSSA,
				NextRevision: 6,
			},
		},
		{
			name:    "superseded-only ssa is upgrade",
			history: []*v1.Release{supersededSSA},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       supersededSSA,
				Current:      supersededSSA,
				NextRevision: 6,
			},
		},
		{
			name:    "superseded-only empty apply method is unsupported",
			history: []*v1.Release{supersededEmpty},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "newest deployed csa is unsupported",
			history: []*v1.Release{prepRelease(1, common.StatusDeployed, applyCSA)},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "newest deployed empty apply method is unsupported",
			history: []*v1.Release{prepRelease(1, common.StatusDeployed, "")},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "newest deployed unknown apply method is unsupported",
			history: []*v1.Release{prepRelease(1, common.StatusDeployed, "json-merge")},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "newest ssa with different current csa is unsupported",
			history: []*v1.Release{olderDeployedCSA, failedSSA},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "newest failed csa with deployed ssa is unsupported",
			history: []*v1.Release{deployed, failedCSA},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
		{
			name:    "uninstalled-only is not install",
			history: []*v1.Release{prepRelease(1, common.StatusUninstalled, applySSA)},
			wantErr: ErrNoDeployedRevision,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := PrepareRelease(tt.history)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, ReleasePrep{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want.Operation, got.Operation)
			assert.Equal(t, tt.want.NextRevision, got.NextRevision)
			assert.Same(t, tt.want.Newest, got.Newest)
			assert.Same(t, tt.want.Current, got.Current)
		})
	}
}

func TestPrepareRelease_DoesNotMutateHistory(t *testing.T) {
	t.Parallel()

	first := prepRelease(1, common.StatusDeployed, applySSA)
	second := prepRelease(2, common.StatusFailed, applySSA)
	history := []*v1.Release{first, second}
	original := slices.Clone(history)

	_, err := PrepareRelease(history)
	require.NoError(t, err)
	assert.Equal(t, original, history)
	assert.Same(t, first, history[0])
	assert.Same(t, second, history[1])
}

func TestPrepareRelease_PendingIsWrappable(t *testing.T) {
	t.Parallel()

	_, err := PrepareRelease([]*v1.Release{
		prepRelease(1, common.StatusPendingUpgrade, applyCSA),
	})
	require.Error(t, err)
	wrapped := fmt.Errorf("history: %w", err)
	assert.ErrorIs(t, wrapped, ErrReleasePending)
}

func TestPrepareReleaseFromHistory(t *testing.T) {
	t.Parallel()

	deployed := prepRelease(1, common.StatusDeployed, applySSA)
	storageErr := errors.New("secret storage unavailable")
	stringNotFound := errors.New("release: not found")
	k8sNotFound := apierrors.NewNotFound(schema.GroupResource{Resource: "secrets"}, "rel.v1")

	tests := []struct {
		name    string
		rels    []*v1.Release
		histErr error
		wantOp  Operation
		wantErr error
	}{
		{
			name:    "driver not found is install",
			histErr: driver.ErrReleaseNotFound,
			wantOp:  OperationInstall,
		},
		{
			name:    "wrapped driver not found is install",
			histErr: fmt.Errorf("history: %w", driver.ErrReleaseNotFound),
			wantOp:  OperationInstall,
		},
		{
			name:    "k8s not found is not install",
			histErr: k8sNotFound,
			wantErr: k8sNotFound,
		},
		{
			name:    "not found string is not install",
			histErr: stringNotFound,
			wantErr: stringNotFound,
		},
		{
			name:    "storage error is not install",
			histErr: storageErr,
			wantErr: storageErr,
		},
		{
			name:   "ssa history is upgrade",
			rels:   []*v1.Release{deployed},
			wantOp: OperationUpgrade,
		},
		{
			name:    "csa history is unsupported",
			rels:    []*v1.Release{prepRelease(1, common.StatusDeployed, applyCSA)},
			wantErr: ErrUnsupportedReleaseApplyMethod,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var histRels []release.Releaser
			for _, rel := range tt.rels {
				histRels = append(histRels, rel)
			}

			got, err := prepareReleaseFromHistory(histRels, tt.histErr)
			if tt.wantErr != nil {
				require.Error(t, err)
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Equal(t, ReleasePrep{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOp, got.Operation)
		})
	}
}

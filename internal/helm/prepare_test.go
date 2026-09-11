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
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/release/common"

	v1 "helm.sh/helm/v4/pkg/release/v1"
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

	deployed := prepRelease(4, common.StatusDeployed, string(ApplyMethodSSA))
	failed := prepRelease(5, common.StatusFailed, string(ApplyMethodCSA))
	superseded := prepRelease(5, common.StatusSuperseded, "")
	olderDeployed := prepRelease(3, common.StatusDeployed, string(ApplyMethodSSA))
	failedSSA := prepRelease(5, common.StatusFailed, string(ApplyMethodSSA))
	deployedCSA := prepRelease(4, common.StatusDeployed, string(ApplyMethodCSA))

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
				ApplyMethod:  ApplyMethodSSA,
				NextRevision: 1,
			},
		},
		{
			name:    "empty history is install",
			history: []*v1.Release{},
			want: ReleasePrep{
				Operation:    OperationInstall,
				ApplyMethod:  ApplyMethodSSA,
				NextRevision: 1,
			},
		},
		{
			name:    "latest deployed is upgrade",
			history: []*v1.Release{prepRelease(1, common.StatusSuperseded, ""), deployed},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       deployed,
				Current:      deployed,
				ApplyMethod:  ApplyMethodSSA,
				NextRevision: 5,
			},
		},
		{
			name:    "unsorted history still picks highest version",
			history: []*v1.Release{deployed, prepRelease(1, common.StatusSuperseded, ""), prepRelease(2, common.StatusSuperseded, "")},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       deployed,
				Current:      deployed,
				ApplyMethod:  ApplyMethodSSA,
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
			history: []*v1.Release{olderDeployed, prepRelease(4, common.StatusPendingUpgrade, "")},
			wantErr: ErrReleasePending,
		},
		{
			name:    "pending-rollback latest",
			history: []*v1.Release{olderDeployed, prepRelease(4, common.StatusPendingRollback, "")},
			wantErr: ErrReleasePending,
		},
		{
			name:    "failed newest with older deployed",
			history: []*v1.Release{olderDeployed, failed},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failed,
				Current:      olderDeployed,
				ApplyMethod:  ApplyMethodCSA,
				NextRevision: 6,
			},
		},
		{
			name:    "superseded newest with older deployed",
			history: []*v1.Release{olderDeployed, superseded},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       superseded,
				Current:      olderDeployed,
				ApplyMethod:  ApplyMethodCSA,
				NextRevision: 6,
			},
		},
		{
			name:    "failed-only is upgrade not install",
			history: []*v1.Release{failed},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failed,
				Current:      failed,
				ApplyMethod:  ApplyMethodCSA,
				NextRevision: 6,
			},
		},
		{
			name:    "superseded-only is upgrade",
			history: []*v1.Release{superseded},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       superseded,
				Current:      superseded,
				ApplyMethod:  ApplyMethodCSA,
				NextRevision: 6,
			},
		},
		{
			name:    "newest failed csa with deployed ssa uses newest",
			history: []*v1.Release{deployed, failed},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failed,
				Current:      deployed,
				ApplyMethod:  ApplyMethodCSA,
				NextRevision: 6,
			},
		},
		{
			name:    "newest failed ssa with deployed csa uses newest",
			history: []*v1.Release{deployedCSA, failedSSA},
			want: ReleasePrep{
				Operation:    OperationUpgrade,
				Newest:       failedSSA,
				Current:      deployedCSA,
				ApplyMethod:  ApplyMethodSSA,
				NextRevision: 6,
			},
		},
		{
			name:    "uninstalled-only is not install",
			history: []*v1.Release{prepRelease(1, common.StatusUninstalled, "")},
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
			assert.Equal(t, tt.want.ApplyMethod, got.ApplyMethod)
			assert.Equal(t, tt.want.NextRevision, got.NextRevision)
			assert.Same(t, tt.want.Newest, got.Newest)
			assert.Same(t, tt.want.Current, got.Current)
		})
	}
}

func TestPrepareRelease_DoesNotMutateHistory(t *testing.T) {
	t.Parallel()

	first := prepRelease(1, common.StatusDeployed, "")
	second := prepRelease(2, common.StatusFailed, "")
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
		prepRelease(1, common.StatusPendingUpgrade, ""),
	})
	require.Error(t, err)
	wrapped := fmt.Errorf("history: %w", err)
	assert.ErrorIs(t, wrapped, ErrReleasePending)
}

func TestApplyMethodFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		op     Operation
		newest string
		want   ApplyMethod
	}{
		{name: "install ignores ssa", op: OperationInstall, newest: "ssa", want: ApplyMethodSSA},
		{name: "install ignores csa", op: OperationInstall, newest: "csa", want: ApplyMethodSSA},
		{name: "install ignores empty", op: OperationInstall, newest: "", want: ApplyMethodSSA},
		{name: "upgrade ssa", op: OperationUpgrade, newest: "ssa", want: ApplyMethodSSA},
		{name: "upgrade csa", op: OperationUpgrade, newest: "csa", want: ApplyMethodCSA},
		{name: "upgrade empty is csa", op: OperationUpgrade, newest: "", want: ApplyMethodCSA},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, applyMethodFor(tt.op, tt.newest))
		})
	}
}

func TestApplyMethodFor_InvalidOperationPanics(t *testing.T) {
	t.Parallel()

	assert.Panics(t, func() {
		applyMethodFor(0, "ssa")
	})
}

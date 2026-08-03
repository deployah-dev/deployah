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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/runtime/schema"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// TestWrapHelmError_TypedClassification verifies typed Helm and Kubernetes
// errors map to the expected sentinels and user-facing messages.
func TestWrapHelmError_TypedClassification(t *testing.T) {
	t.Parallel()
	c := &Client{}

	tests := []struct {
		name    string
		err     error
		wantIs  error
		wantMsg string
	}{
		{
			name:   "helm driver not found",
			err:    driver.ErrReleaseNotFound,
			wantIs: ErrReleaseNotFound,
		},
		{
			name:   "helm driver already exists",
			err:    driver.ErrReleaseExists,
			wantIs: ErrReleaseAlreadyExists,
		},
		{
			name: "k8s not found",
			err: apierrors.NewNotFound(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
			),
			wantIs: ErrReleaseNotFound,
		},
		{
			name: "k8s already exists",
			err: apierrors.NewAlreadyExists(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
			),
			wantIs: ErrReleaseAlreadyExists,
		},
		{
			name: "k8s forbidden",
			err: apierrors.NewForbidden(
				schema.GroupResource{Resource: "secrets"}, "myrel.v1",
				errors.New("denied"),
			),
			wantMsg: "insufficient permissions",
		},
		{
			name:    "k8s unauthorized",
			err:     apierrors.NewUnauthorized("bad token"),
			wantMsg: "insufficient permissions",
		},
		{
			name:    "k8s timeout",
			err:     apierrors.NewTimeoutError("slow", 1),
			wantMsg: "operation timed out",
		},
		{
			name: "net op error",
			err: &net.OpError{
				Op:  "dial",
				Net: "tcp",
				Err: errors.New("connection refused"),
			},
			wantMsg: "unable to connect to Kubernetes cluster",
		},
		{
			name:   "helm pending string",
			err:    errors.New("another operation (install/upgrade/rollback) is in progress"),
			wantIs: ErrReleasePending,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := c.wrapHelmError("upgrade", "app-prod", tt.err)
			require.Error(t, got)
			if tt.wantIs != nil {
				assert.ErrorIs(t, got, tt.wantIs)
			}
			if tt.wantMsg != "" {
				assert.ErrorContains(t, got, tt.wantMsg)
			}
			// Original error remains inspectable through the wrap.
			assert.ErrorIs(t, got, tt.err)
		})
	}
}

// TestWrapHelmError_PreservesWrappedCause verifies classification sees through
// a wrapping layer to the underlying Kubernetes API error.
func TestWrapHelmError_PreservesWrappedCause(t *testing.T) {
	t.Parallel()
	c := &Client{}
	cause := apierrors.NewForbidden(
		schema.GroupResource{Resource: "secrets"}, "x", errors.New("no"),
	)
	wrapped := fmt.Errorf("helm failed: %w", cause)
	got := c.wrapHelmError("install", "app-prod", wrapped)
	assert.ErrorIs(t, got, cause)
	assert.ErrorContains(t, got, "insufficient permissions")
}

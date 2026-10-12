// Copyright 2026 The Deployah Authors
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

package e2e

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanCluster(t *testing.T) {
	t.Parallel()
	lookupErr := errors.New("timeout")
	tests := []struct {
		name  string
		found bool
		err   error
		force bool
		want  ClusterIntent
	}{
		{name: "absent", want: ClusterCreate},
		{name: "exists without force", found: true, want: ClusterRefuse},
		{name: "exists with force", found: true, force: true, want: ClusterReplace},
		{name: "lookup error", err: lookupErr, want: ClusterUncertain},
		{name: "lookup error ignores force", err: lookupErr, force: true, want: ClusterUncertain},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, PlanCluster(tt.found, tt.err, tt.force))
		})
	}
}

type fakeCleanup struct {
	fns    []func()
	errors []string
}

func (f *fakeCleanup) Helper()           {}
func (f *fakeCleanup) Cleanup(fn func()) { f.fns = append(f.fns, fn) }
func (f *fakeCleanup) Errorf(format string, args ...any) {
	f.errors = append(f.errors, fmt.Sprintf(format, args...))
}

func TestPrepareCluster(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	tests := []struct {
		name     string
		intent   ClusterIntent
		upErr    error
		downErr  error
		wantErr  string
		wantDown string
		wantLog  []string
		cleanups int
	}{
		{name: "create then clean up", intent: ClusterCreate, wantLog: []string{"up", "down"}, cleanups: 1},
		{name: "create failure still cleans up", intent: ClusterCreate, upErr: errors.New("status"), wantErr: "status", wantLog: []string{"up", "down"}, cleanups: 1},
		{name: "cleanup reports a down error", intent: ClusterCreate, downErr: boom, wantDown: "cluster down: boom", wantLog: []string{"up", "down"}, cleanups: 1},
		{name: "replace deletes before create", intent: ClusterReplace, wantLog: []string{"down", "up", "down"}, cleanups: 1},
		{name: "refuse does not delete", intent: ClusterRefuse, wantErr: "cluster already exists", cleanups: 0},
		{name: "uncertain does not delete", intent: ClusterUncertain, wantErr: "cluster lookup failed", cleanups: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var log []string
			fake := &fakeCleanup{}
			err := PrepareCluster(fake, tt.intent,
				func() error { log = append(log, "down"); return tt.downErr },
				func() error { log = append(log, "up"); return tt.upErr },
			)
			gotErr := ""
			if err != nil {
				gotErr = err.Error()
			}
			require.Len(t, fake.fns, tt.cleanups)
			for _, fn := range fake.fns {
				fn()
			}
			for _, fn := range fake.fns {
				fn()
			}
			assert.Equal(t, tt.wantErr, gotErr)
			assert.Equal(t, tt.wantLog, log)
			assert.Equal(t, tt.wantDown, strings.Join(fake.errors, "\n"))
		})
	}
}

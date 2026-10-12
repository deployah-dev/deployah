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
	"sync"
)

var (
	errClusterExists  = errors.New("cluster already exists")
	errClusterUnknown = errors.New("cluster lookup failed")
)

// ClusterIntent is what this process may do with a named Kind cluster.
type ClusterIntent int

const (
	// ClusterCreate means the name is free. Register cleanup, then create.
	ClusterCreate ClusterIntent = iota
	// ClusterReplace means force was set. Delete the old cluster, then create.
	ClusterReplace
	// ClusterRefuse means the name is taken and this run must not delete it.
	ClusterRefuse
	// ClusterUncertain means the lookup failed. Leave the name alone.
	ClusterUncertain
)

// PlanCluster decides whether this run may create or delete a Kind cluster.
func PlanCluster(found bool, getErr error, force bool) ClusterIntent {
	// A lookup error stays uncertain even when found is also set.
	if getErr != nil {
		return ClusterUncertain
	}
	if found && !force {
		return ClusterRefuse
	}
	if found && force {
		return ClusterReplace
	}
	return ClusterCreate
}

// testCleanup is the part of a test used to register teardown.
type testCleanup interface {
	Helper()
	Cleanup(func())
	Errorf(string, ...any)
}

// OwnCluster registers down once, before create.
func OwnCluster(tb testCleanup, down func() error) {
	tb.Helper()
	// down still runs when the test stops early.
	var once sync.Once
	tb.Cleanup(func() {
		once.Do(func() {
			if down == nil {
				return
			}
			if err := down(); err != nil {
				tb.Errorf("cluster down: %v", err)
			}
		})
	})
}

// PrepareCluster runs a PlanCluster result.
func PrepareCluster(tb testCleanup, intent ClusterIntent, down, up func() error) error {
	tb.Helper()
	// Cleanup is registered before create. Refuse and uncertain do not delete.
	switch intent {
	case ClusterRefuse:
		return errClusterExists
	case ClusterUncertain:
		return errClusterUnknown
	case ClusterReplace:
		if err := down(); err != nil {
			return err
		}
		fallthrough
	case ClusterCreate:
		OwnCluster(tb, down)
		if up == nil {
			return nil
		}
		return up()
	default:
		return errClusterUnknown
	}
}

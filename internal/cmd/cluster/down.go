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

package cluster

import (
	"errors"
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/localkube"
)

// downOptions holds command-line flags for "cluster down".
type downOptions struct {
	Force bool `nabat:"force"`
}

// registerDown attaches the "down" subcommand to the cluster group.
func registerDown(group *nabat.Command) {
	group.MustCommand("down",
		nabat.WithDescription("Delete the local cluster and stop the cloud provider"),
		nabat.WithLongDescription("Stop the cloud-provider-kind container (if running) and delete the local cluster. "+
			"This is destructive and removes all workloads running in the cluster."),
		nabat.WithFlag("force", false, nabat.WithUsage("Delete without confirmation")),
		nabat.WithExample(`
# Delete the local cluster (asks for confirmation)
deployah cluster down

# Delete without confirmation
deployah cluster down --force`),
		nabat.WithRun(runDown),
	)
}

func runDown(c *nabat.Context) error {
	opts := &downOptions{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	m, err := newManager(c)
	if err != nil {
		return fmt.Errorf("init local cluster manager: %w", err)
	}
	defer closeManager(c, m)

	// Nothing to delete if the cluster isn't there; skip the prompt entirely.
	if _, getErr := m.Get(c, clusterName); errors.Is(getErr, localkube.ErrNotFound) {
		c.Info("No local cluster found", "hint", "run 'deployah cluster up' to create one")
		return nil
	}

	if !opts.Force {
		confirmed, confirmErr := c.Confirm(
			"Delete the local cluster? This removes all workloads running in it.",
			nabat.WithAffirmative("Yes, delete it"),
			nabat.WithNegative("No, cancel"),
		)
		if confirmErr != nil {
			return fmt.Errorf("confirmation: %w", confirmErr)
		}
		if !confirmed {
			c.Info("Delete cancelled")
			return nil
		}
	}

	// Stop the cloud provider container first; ignore ErrUnsupported (engine mismatch).
	if spinErr := c.Spinner("Stopping cloud provider...", func(_ *nabat.Spinner) error {
		stopErr := m.StopCloudProvider(c, localkube.WithClusterName(clusterName))
		if stopErr != nil && !errors.Is(stopErr, localkube.ErrUnsupported) {
			return stopErr
		}
		return nil
	}); spinErr != nil {
		return fmt.Errorf("stop cloud provider: %w", spinErr)
	}

	if spinErr := c.Spinner("Deleting local cluster", func(sp *nabat.Spinner) error {
		return m.Delete(c, clusterName,
			localkube.WithIgnoreMissing(),
			localkube.WithDeleteEventHandler(func(e localkube.Event) {
				if e.Status == localkube.StepStarted {
					if lbl := phaseLabel(e.Step); lbl != "" {
						sp.SetText("Deleting local cluster: " + lbl)
					}
				}
			}),
		)
	}); spinErr != nil {
		return fmt.Errorf("delete local cluster: %w", spinErr)
	}

	c.Success("Local cluster deleted", "cluster", clusterName)
	return nil
}

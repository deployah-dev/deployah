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
	"strings"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/localkube"
)

// kubeconfigOptions holds command-line flags for "cluster kubeconfig".
type kubeconfigOptions struct {
	Raw bool `nabat:"raw"`
}

// registerKubeconfig attaches the "kubeconfig" subcommand to the cluster group.
func registerKubeconfig(group *nabat.Command) {
	group.MustCommand("kubeconfig",
		nabat.WithDescription("Print the local cluster kubeconfig path or contents"),
		nabat.WithLongDescription("Print the path to the deployah-managed kubeconfig for the local cluster. Use --raw "+
			"to print the kubeconfig YAML contents instead, which is handy for piping into other tools."),
		nabat.WithFlag("raw", false, nabat.WithUsage("Print the kubeconfig YAML contents instead of its path")),
		nabat.WithExample(`
# Print the kubeconfig path (script-friendly)
deployah deploy local --kubeconfig "$(deployah cluster kubeconfig)"

# Print the kubeconfig contents
deployah cluster kubeconfig --raw`),
		nabat.WithRun(runKubeconfig),
	)
}

func runKubeconfig(c *nabat.Context) error {
	opts := &kubeconfigOptions{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	m, err := newManager(c)
	if err != nil {
		return fmt.Errorf("init local cluster manager: %w", err)
	}

	// Probe existence first so a missing cluster fails with a clean, actionable
	// error instead of dumping kind internals (and without a StepFailed event).
	if _, getErr := m.Get(c, clusterName); errors.Is(getErr, localkube.ErrNotFound) {
		return fmt.Errorf("no local cluster found; run 'deployah cluster up' to create one")
	}

	kc, err := m.KubeConfig(c, clusterName)
	if err != nil {
		return fmt.Errorf("read kubeconfig: %w", err)
	}

	if opts.Raw {
		c.Println(strings.TrimRight(string(kc.Bytes()), "\n"))
		return nil
	}

	c.Println(kc.Path())
	return nil
}

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
	"log/slog"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/localkube"
)

// LocalKubeconfigPath returns the path where deployah stores the kubeconfig
// for the managed local cluster. The file may not exist when no cluster
// is running.
func LocalKubeconfigPath() (string, error) {
	return localkube.DefaultKubeconfigPath(clusterName)
}

// clusterName is the fixed name of the single local cluster managed by the
// "deployah cluster" command group. Developers never type it; the matching
// Kubernetes context is "kind-deployah".
const clusterName = "deployah"

// Register adds the "cluster" command group and its subcommands to app.
func Register(app *nabat.App) {
	group := app.MustCommand("cluster",
		nabat.WithDescription("Manage a local Kubernetes cluster for development"),
		nabat.WithLongDescription("Manage a single local Kubernetes cluster (backed by Kind) for development.\n"+
			"The cluster lifecycle is independent of deployah environments. Bring the cluster up, "+
			"then point a deploy at it with the global --context flag or an environment's \"context\" field "+
			"(the cluster's context is \"kind-deployah\")."),
	)

	registerUp(group)
	registerDown(group)
	registerStatus(group, app)
	registerKubeconfig(group)
}

// closeManager shuts down m, logging any error without failing the command.
func closeManager(c *nabat.Context, m *localkube.Manager) {
	if err := m.Close(); err != nil {
		c.Logger().Warn("close local cluster manager failed", "err", err)
	}
}

// newManager builds a localkube.Manager wired to the command's logger and a
// progress-event handler. Callers pass extra options (Kubernetes version,
// runtime) as needed.
func newManager(c *nabat.Context, extra ...localkube.Option) (*localkube.Manager, error) {
	log := c.Logger()
	opts := append([]localkube.Option{
		localkube.WithLogger(log),
		localkube.WithEventHandler(logEvent(log)),
	}, extra...)
	return localkube.New(opts...)
}

// runMaybeSpinner runs fn, optionally wrapped in a spinner. When spin is true
// it delegates to Context.Spinner; otherwise it calls fn directly with a nil
// handle. The no-spinner path is used for fast idempotent no-ops, where the
// spinner would add visual noise and (because it starts a Bubble Tea program)
// can leak terminal capability-probe replies onto the next shell prompt.
//
// Callbacks must guard handle use with a nil check, since fn receives nil on
// the no-spinner path.
func runMaybeSpinner(c *nabat.Context, title string, spin bool, fn func(*nabat.Spinner) error) error {
	if spin {
		return c.Spinner(title, fn)
	}
	return fn(nil)
}

// phaseLabel maps a localkube Step to a short human-readable phrase for use
// as a spinner subtitle. Returns "" for steps that don't need a subtitle
// (e.g. the top-level StepCreating, whose base title already covers it).
func phaseLabel(s localkube.Step) string {
	switch s {
	case localkube.StepPullingNodeImage:
		return "pulling node image"
	case localkube.StepPreparingNodes:
		return "preparing nodes"
	case localkube.StepWritingConfiguration:
		return "writing configuration"
	case localkube.StepStartingControlPlane:
		return "starting control plane"
	case localkube.StepInstallingCNI:
		return "installing CNI"
	case localkube.StepInstallingStorageClass:
		return "installing storage class"
	case localkube.StepJoiningControlPlane:
		return "joining control-plane nodes"
	case localkube.StepJoiningWorkers:
		return "joining worker nodes"
	case localkube.StepWaitingForReady:
		return "waiting for nodes to be ready"
	case localkube.StepDeleting:
		return "removing cluster"
	default:
		return ""
	}
}

// logEvent bridges localkube progress events to the structured logger. Step
// start/done are logged at debug so a spinner stays clean; failures are logged
// at error with the underlying error attached.
func logEvent(log *slog.Logger) localkube.EventFunc {
	return func(e localkube.Event) {
		attrs := []any{"step", e.Step}
		if e.Detail != "" {
			attrs = append(attrs, "detail", e.Detail)
		}
		switch e.Status {
		case localkube.StepStarted:
			log.Debug("step started", attrs...)
		case localkube.StepCompleted:
			log.Debug("step done", attrs...)
		case localkube.StepFailed:
			if e.Err != nil {
				attrs = append(attrs, "err", e.Err)
			}
			log.Error("step failed", attrs...)
		}
	}
}

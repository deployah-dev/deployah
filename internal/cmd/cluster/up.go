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
	"os/signal"
	"syscall"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/localkube"
)

// upOptions holds command-line flags for "cluster up".
type upOptions struct {
	NoCloudProvider   bool   `nabat:"no-cloud-provider"`
	Attach            bool   `nabat:"attach"`
	KubernetesVersion string `nabat:"kubernetes-version"`
	Runtime           string `nabat:"runtime"`
	SyncRegistryAuth  bool   `nabat:"sync-registry-auth"`
}

// runtimeOptions are the accepted values for the --runtime flag.
var runtimeOptions = []string{"auto", "docker", "podman", "nerdctl"}

// registerUp attaches the "up" subcommand to the cluster group.
func registerUp(group *nabat.Command) {
	group.MustCommand("up",
		nabat.WithDescription("Create the local cluster and start the cloud provider"),
		nabat.WithLongDescription("Create the local cluster if it does not exist, write its kubeconfig, and start the "+
			"cloud-provider-kind container so that LoadBalancer Services and Ingress work. By default this command "+
			"returns immediately after starting the cloud provider in the background.\n\n"+
			"Use --attach to stay in the foreground and stream the cloud provider logs; the container is stopped "+
			"when you press Ctrl-C.\n\n"+
			"Use --no-cloud-provider to only create the cluster without starting the cloud provider."),
		nabat.WithFlag("no-cloud-provider", false, nabat.WithUsage("Only create the cluster; do not start the cloud provider")),
		nabat.WithFlag("attach", false, nabat.WithUsage("Stay in the foreground and stream cloud provider logs (Ctrl-C stops the container)")),
		nabat.WithFlag("kubernetes-version", "", nabat.WithUsage("Kubernetes version for the cluster (e.g. 1.31 or v1.31.2)")),
		nabat.WithSelectFlag("runtime", "auto", runtimeOptions, nabat.WithUsage("Host container engine to use")),
		nabat.WithFlag("sync-registry-auth", false, nabat.WithUsage("Copy host registry credentials into the cluster as a Kubernetes Secret and patch the default ServiceAccount to use them")),
		nabat.WithExample(`
# Bring up the local cluster with cloud provider in the background
deployah cluster up

# Create the cluster only (no cloud provider)
deployah cluster up --no-cloud-provider

# Foreground mode: stream cloud provider logs, Ctrl-C stops the container
deployah cluster up --attach

# Pin the Kubernetes version and force a runtime
deployah cluster up --kubernetes-version 1.31 --runtime podman`),
		nabat.WithRun(runUp),
	)
}

func runUp(c *nabat.Context) error {
	opts := &upOptions{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	rt, err := parseRuntime(opts.Runtime)
	if err != nil {
		return err
	}

	mgrOpts := []localkube.Option{localkube.WithRuntime(rt)}
	if opts.KubernetesVersion != "" {
		mgrOpts = append(mgrOpts, localkube.WithKubernetesVersion(opts.KubernetesVersion))
	}

	m, err := newManager(c, mgrOpts...)
	if err != nil {
		return fmt.Errorf("init local cluster manager: %w", err)
	}
	defer closeManager(c, m)

	// Probe current state so the output can tell the user whether this run
	// actually created anything or the cluster was already there. Get returns
	// ErrNotFound when the cluster is absent, so a nil error means it exists.
	_, getErr := m.Get(c, clusterName)
	clusterExisted := getErr == nil

	// Skip the spinner when the cluster already exists: Create is then a fast
	// idempotent no-op, so the spinner only adds noise (and risks leaking
	// terminal probe replies on the quick path).
	const createTitle = "Creating local cluster"
	if spinErr := runMaybeSpinner(c, createTitle, !clusterExisted, func(sp *nabat.Spinner) error {
		return m.Create(c, clusterName,
			localkube.WithCreateIfMissing(),
			localkube.WithCreateEventHandler(func(e localkube.Event) {
				if sp == nil || e.Status != localkube.StepStarted {
					return
				}
				if lbl := phaseLabel(e.Step); lbl != "" {
					sp.SetText(createTitle + ": " + lbl)
				}
			}),
		)
	}); spinErr != nil {
		return fmt.Errorf("create local cluster: %w", spinErr)
	}

	kc, err := m.KubeConfig(c, clusterName)
	if err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}

	if opts.SyncRegistryAuth {
		if syncErr := m.SyncRegistryAuth(c, clusterName, "default"); syncErr != nil {
			// Non-fatal: warn and continue so the rest of cluster up succeeds.
			c.Logger().Warn("registry auth sync failed", "err", syncErr)
		} else {
			c.Logger().Debug("registry credentials synced to cluster",
				"secret", "deployah-registry-auth", "namespace", "default")
		}
	}

	ctxName := m.ContextName(clusterName)

	// nextSteps prints the action hints as a single grouped block, separated
	// from the status lines above by a blank line. Commands are aligned so they
	// read as copy-pasteable instructions rather than dense key=value pairs.
	// The stop hint is omitted in foreground (--attach) mode, where Ctrl-C is
	// the way to stop.
	nextSteps := func(includeStop bool) {
		if _, writeErr := fmt.Fprintln(c.IO().ErrOut); writeErr != nil {
			c.Logger().Warn("write output separator failed", "err", writeErr)
		}
		c.Info("Deploy:  deployah deploy <environment>  (set context: " + ctxName + " in your environment, or pass --context " + ctxName + ")")
		c.Info(`kubectl: export KUBECONFIG="$(deployah cluster kubeconfig)"`)
		if includeStop {
			c.Info("Stop:    deployah cluster down")
		}
	}

	if clusterExisted {
		c.Success("Local cluster already exists", "context", ctxName, "kubeconfig", kc.Path())
	} else {
		c.Success("Local cluster ready", "context", ctxName, "kubeconfig", kc.Path())
	}

	if opts.NoCloudProvider {
		c.Info("Cloud provider disabled; LoadBalancer and Ingress will not be reachable")
		nextSteps(true)
		return nil
	}

	if opts.Attach {
		// Foreground: wire Ctrl-C, then stream logs until interrupted. Print the
		// hints first since AttachCloudProvider blocks until the user stops it.
		ctx, stop := signal.NotifyContext(c, syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		nextSteps(false)
		c.Info("Streaming cloud provider logs in the foreground", "note", "run deploy/kubectl from another terminal; press Ctrl-C to stop")
		if attachErr := m.AttachCloudProvider(ctx); attachErr != nil && !errors.Is(attachErr, c.Err()) {
			return fmt.Errorf("cloud provider: %w", attachErr)
		}
		c.Info("Cloud provider stopped", "cluster", clusterName)
		return nil
	}

	// Background: start container and return immediately. Capture the prior
	// state first so the message reflects whether we actually started it, and
	// skip the spinner when it is already running (StartCloudProvider is then a
	// fast no-op).
	cloudWasRunning := m.CloudProviderRunning(c)
	if startErr := runMaybeSpinner(c, "Starting cloud provider...", !cloudWasRunning, func(_ *nabat.Spinner) error {
		return m.StartCloudProvider(c)
	}); startErr != nil {
		if errors.Is(startErr, localkube.ErrUnsupported) {
			c.Info("Cloud provider not supported on this runtime; LoadBalancer and Ingress will not be reachable")
			nextSteps(true)
			return nil
		}
		return fmt.Errorf("start cloud provider: %w", startErr)
	}

	// Group both status lines together, then the action block.
	if cloudWasRunning {
		c.Success("Cloud provider already running")
	} else {
		c.Success("Cloud provider running in the background")
	}
	nextSteps(true)
	return nil
}

// parseRuntime converts the --runtime flag value to a localkube.Runtime.
func parseRuntime(name string) (localkube.Runtime, error) {
	switch name {
	case "", "auto":
		return localkube.RuntimeAuto, nil
	case "docker":
		return localkube.RuntimeDocker, nil
	case "podman":
		return localkube.RuntimePodman, nil
	case "nerdctl":
		return localkube.RuntimeNerdctl, nil
	default:
		return localkube.RuntimeAuto, fmt.Errorf("unsupported runtime %q (want one of: auto, docker, podman, nerdctl)", name)
	}
}

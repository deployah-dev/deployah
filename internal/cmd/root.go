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

// Package cmd provides the commands for the Deployah application.
package cmd

import (
	"context"
	"fmt"
	"os"

	"nabat.dev/logging"
	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/cmd/cluster"
	"deployah.dev/deployah/internal/cmd/common"
	"deployah.dev/deployah/internal/cmd/delete"
	"deployah.dev/deployah/internal/cmd/deploy"
	"deployah.dev/deployah/internal/cmd/initialize"
	"deployah.dev/deployah/internal/cmd/list"
	"deployah.dev/deployah/internal/cmd/logs"
	"deployah.dev/deployah/internal/cmd/shell"
	"deployah.dev/deployah/internal/cmd/status"
	"deployah.dev/deployah/internal/cmd/validate"
	"deployah.dev/deployah/internal/manifest"
	"deployah.dev/deployah/internal/runtime"
)

var version = "dev"

// NewApp creates a new Nabat application with all subcommands registered.
func NewApp() *nabat.App {
	app := nabat.MustNew("deployah",
		nabat.WithTheme("gruvbox"),
		nabat.WithVersion(version),
		nabat.WithDescription("Deployah turns a spec into a running release on Kubernetes (Spec-to-Release)"),
		nabat.WithLongDescription("Deployah is a Spec-to-Release tool. You write a short spec; Deployah renders and installs the Helm release, so you do not write Helm charts or Kubernetes YAML."),
		nabat.WithCompletion(nabat.WithCompletionHidden()),
		nabat.WithFlag("debug", false, nabat.WithShort('d'), nabat.WithUsage("Enable debug mode (verbose logging and keep temporary files)"), nabat.WithPersistent()),
		nabat.WithFlag("spec", manifest.DefaultManifestPath, nabat.WithShort('s'), nabat.WithUsage("Path to the Deployah spec file (YAML or JSON)"), nabat.WithPersistent()),
		nabat.WithFlag("namespace", "", nabat.WithShort('n'), nabat.WithUsage("Kubernetes namespace to use for Deployah operations (defaults to current context namespace)"), nabat.WithPersistent()),
		nabat.WithFlag("kubeconfig", "", nabat.WithShort('k'), nabat.WithUsage("Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)"), nabat.WithPersistent()),
		nabat.WithFlag("context", "", nabat.WithUsage("Kubernetes context to use (overrides the current context and any environment 'context' field)"), nabat.WithPersistent()),
		nabat.WithFlag("timeout", runtime.DefaultTimeout, nabat.WithShort('t'), nabat.WithUsage("Timeout for Deployah operations (install/upgrade, list, status, logs, delete)"), nabat.WithPersistent()),
		nabat.WithExtension(logging.New(logging.WithVerboseFlag("debug"))),
	)

	// Build runtime once from global flags and store in context for all commands.
	if err := app.OnPreRun(func(c *nabat.Context) error {
		var opts common.GlobalOptions
		if err := c.Bind(&opts); err != nil {
			return fmt.Errorf("binding global options: %w", err)
		}

		// Make the local cluster's kubeconfig discoverable without polluting
		// ~/.kube/config. Missing file is silently skipped by client-go.
		localKubeconfig, err := cluster.LocalKubeconfigPath()
		if err != nil {
			localKubeconfig = ""
		}

		rtOpts := []runtime.Option{
			runtime.WithNamespace(opts.Namespace),
			runtime.WithKubeconfig(opts.Kubeconfig),
			runtime.WithKubeContext(opts.Context),
			runtime.WithManifestPath(opts.Spec),
			runtime.WithDebug(opts.Debug),
			runtime.WithTimeout(opts.Timeout),
		}
		if localKubeconfig != "" {
			rtOpts = append(rtOpts, runtime.WithExtraKubeconfigPaths(localKubeconfig))
		}

		rt := runtime.New(rtOpts...)
		c.SetContext(runtime.WithContext(c.Context(), rt))
		return nil
	}); err != nil {
		panic(fmt.Sprintf("failed to register OnPreRun hook: %v", err))
	}

	// Subcommands (alphabetical)
	cluster.Register(app)
	delete.Register(app)
	deploy.Register(app)
	initialize.Register(app)
	list.Register(app)
	logs.Register(app)
	shell.Register(app)
	status.Register(app)
	validate.Register(app)

	return app
}

// Execute is the main entry point for the Deployah application.
func Execute() {
	app := NewApp()
	if err := app.Run(context.Background()); err != nil {
		os.Exit(1)
	}
}

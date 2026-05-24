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

// NewApp creates a new Nabat application with all subcommands registered.
func NewApp() *nabat.App {
	app := nabat.MustNew("deployah",
		nabat.WithTheme("gruvbox"),
		nabat.WithVersion("0.1.0"),
		nabat.WithDescription("Deployah is a tool for deploying applications to Kubernetes"),
		nabat.WithLongDescription("Deployah is a simple deployment tool that can be used to deploy applications to Kubernetes clusters."),
		nabat.WithCompletion(nabat.WithCompletionHidden()),
		nabat.WithFlag("debug", false, nabat.WithShort('d'), nabat.WithUsage("Enable debug mode (verbose logging and keep temporary files)"), nabat.WithPersistent()),
		nabat.WithFlag("config", manifest.DefaultManifestPath, nabat.WithShort('c'), nabat.WithUsage("Path to Deployah configuration file (YAML or JSON)"), nabat.WithPersistent()),
		nabat.WithFlag("namespace", "", nabat.WithShort('n'), nabat.WithUsage("Kubernetes namespace to use for Deployah operations (defaults to current context namespace)"), nabat.WithPersistent()),
		nabat.WithFlag("kubeconfig", "", nabat.WithShort('k'), nabat.WithUsage("Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)"), nabat.WithPersistent()),
		nabat.WithFlag("timeout", runtime.DefaultTimeout, nabat.WithShort('t'), nabat.WithUsage("Timeout for Deployah operations (install/upgrade, list, status, logs, delete)"), nabat.WithPersistent()),
		nabat.WithExtension(logging.New(logging.WithVerboseFlag("debug"))),
	)

	// Build runtime once from global flags and store in context for all commands.
	if err := app.OnPreRun(func(c *nabat.Context) error {
		var opts common.GlobalOptions
		if err := c.Bind(&opts); err != nil {
			return fmt.Errorf("binding global options: %w", err)
		}
		rt := runtime.New(
			runtime.WithNamespace(opts.Namespace),
			runtime.WithKubeconfig(opts.Kubeconfig),
			runtime.WithManifestPath(opts.Config),
			runtime.WithDebug(opts.Debug),
			runtime.WithTimeout(opts.Timeout),
		)
		c.SetContext(runtime.WithContext(c.Context(), rt))
		return nil
	}); err != nil {
		panic(fmt.Sprintf("failed to register OnPreRun hook: %v", err))
	}

	// Subcommands (alphabetical)
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

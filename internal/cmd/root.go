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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/fang"
	"github.com/charmbracelet/log"
	"github.com/deployah-dev/deployah/internal/cli"
	"github.com/deployah-dev/deployah/internal/cmd/delete"
	"github.com/deployah-dev/deployah/internal/cmd/deploy"
	"github.com/deployah-dev/deployah/internal/cmd/initilaize"
	"github.com/deployah-dev/deployah/internal/cmd/list"
	"github.com/deployah-dev/deployah/internal/cmd/logs"
	"github.com/deployah-dev/deployah/internal/cmd/shell"
	"github.com/deployah-dev/deployah/internal/cmd/status"
	"github.com/deployah-dev/deployah/internal/cmd/validate"
	"github.com/deployah-dev/deployah/internal/logging"
	"github.com/deployah-dev/deployah/internal/manifest"
	"github.com/deployah-dev/deployah/internal/runtime"
	"github.com/spf13/cobra"
)

// NewRootCommand creates a new root command for the Deployah application. The root command is the main entry point for the application.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "deployah",
		Short: "Deployah is a simple deployment tool",
		Long:  "Deployah is a simple deployment tool that can be used to deploy applications to Kubernetes clusters.",
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			debug, err := cmd.Flags().GetBool("debug")
			if err != nil {
				return fmt.Errorf("failed to get debug flag: %w", err)
			}

			noColor, err := cmd.Flags().GetBool("no-color")
			if err != nil {
				return fmt.Errorf("failed to get no-color flag: %w", err)
			}

			quiet, err := cmd.Flags().GetBool("quiet")
			if err != nil {
				return fmt.Errorf("failed to get quiet flag: %w", err)
			}

			// Set log level based on debug flag
			var logLevel log.Level = log.InfoLevel
			if debug {
				logLevel = log.DebugLevel
			}

			// SilenceErrors sets whether to silence errors from the command.
			// When quiet is true, SilenceErrors is set to true to prevent showing usage when a subcommand returns an error.
			cmd.SilenceErrors = quiet
			cmd.SilenceUsage = true

			if err := logging.SetupCharmLogger(cmd, logLevel.String(), noColor, quiet); err != nil {
				return fmt.Errorf("failed to setup logger: %w", err)
			}

			// Extract flag values and create runtime with options
			namespace, err := cmd.Flags().GetString("namespace")
			if err != nil {
				return fmt.Errorf("failed to get namespace: %w", err)
			}
			kubeconfig, err := cmd.Flags().GetString("kubeconfig")
			if err != nil {
				return fmt.Errorf("failed to get kubeconfig: %w", err)
			}
			manifestPath, err := cmd.Flags().GetString("config")
			if err != nil {
				return fmt.Errorf("failed to get config: %w", err)
			}
			storageDriver, err := cmd.Flags().GetString("storage-driver")
			if err != nil {
				return fmt.Errorf("failed to get storage driver: %w", err)
			}
			timeout, err := cmd.Flags().GetDuration("timeout")
			if err != nil {
				return fmt.Errorf("failed to get timeout: %w", err)
			}

			// Get logger and create adapter for runtime
			logger := logging.GetLogger(cmd)
			loggerAdapter := runtime.NewLoggerAdapter(logger)

			rt := runtime.New(
				runtime.WithNamespace(namespace),
				runtime.WithKubeconfig(kubeconfig),
				runtime.WithManifestPath(manifestPath),
				runtime.WithStorageDriver(storageDriver),
				runtime.WithDebug(debug), // Use debug flag directly
				runtime.WithTimeout(timeout),
				runtime.WithLogger(loggerAdapter),
			)
			cmd.SetContext(runtime.WithRuntime(cmd.Context(), rt))
			return nil
		},
	}

	rootCmd.PersistentFlags().Bool("debug", false, "Enable debug mode (verbose logging and keep temporary files)")
	rootCmd.PersistentFlags().Bool("no-color", false, "If specified, output won't contain any color.")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Quiet or silent mode. Do not show logs or error messages.")
	rootCmd.PersistentFlags().StringP("config", "c", manifest.DefaultManifestPath, "Path to Deployah configuration file (YAML or JSON)")
	rootCmd.PersistentFlags().String("namespace", "", "Kubernetes namespace to use for Deployah operations (defaults to current context namespace)")
	rootCmd.PersistentFlags().String("kubeconfig", "", "Path to the kubeconfig file to use (defaults to standard kubeconfig resolution)")
	rootCmd.PersistentFlags().String("storage-driver", runtime.DefaultStorageDriver, fmt.Sprintf("Storage driver to use (%s) for Deployah releases", strings.Join(runtime.GetValidStorageDrivers(), "|")))
	rootCmd.PersistentFlags().Duration("timeout", runtime.DefaultTimeout, "Timeout for Deployah operations (install/upgrade, list, status, logs, delete)")

	return rootCmd
}

// Execute is the main entry point for the Deployah application.
func Execute() {
	rootCmd := NewRootCommand()
	rootCmd.AddCommand(
		initilaize.New(),
		validate.New(),
		deploy.New(),
		list.New(),
		delete.New(),
		status.New(),
		logs.New(),
		shell.New(),
	)

	if err := fang.Execute(context.Background(), rootCmd,
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			os.Exit(cli.ExitTimedOut)
		}

		os.Exit(cli.ExitError)
	}
}

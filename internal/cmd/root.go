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
	"os"

	"github.com/charmbracelet/fang"
	"github.com/spf13/cobra"
)

const (
	// ExitError is the exit code used when the application encounters an error.
	ExitError = 1

	// ExitTimedOut is the exit code used when the application times out.
	ExitTimedOut = 124
)

// NewRootCommand creates a new root command for the Deployah application. The root command is the main entry point for the application.
func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "deployah",
		Short: "Deployah is a simple deployment tool",
		Long:  `Deployah is a simple deployment tool that can be used to deploy applications to Kubernetes clusters.`,
		CompletionOptions: cobra.CompletionOptions{
			HiddenDefaultCmd: true,
		},
		PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
			logLevel, _ := cmd.Flags().GetString("log-level")
			noColor, _ := cmd.Flags().GetBool("no-color")
			quiet, _ := cmd.Flags().GetBool("quiet")

			// SilenceErrors sets whether to silence errors from the command.
			// When quiet is true, SilenceErrors is set to true to prevent showing usage when a subcommand returns an error.
			cmd.SilenceErrors = quiet
			cmd.SilenceUsage = true

			return setupCharmLogger(cmd, logLevel, noColor, quiet)
		},
	}

	rootCmd.PersistentFlags().StringP("log-level", "l", "info", "Set the logging level (trace|debug|info|warn|error|fatal|panic)")
	rootCmd.PersistentFlags().Bool("no-color", false, "If specified, output won't contain any color.")
	rootCmd.PersistentFlags().BoolP("quiet", "q", false, "Quiet or silent mode. Do not show logs or error messages.")

	return rootCmd
}

// Execute is the main entry point for the Deployah application.
func Execute() {
	rootCmd := NewRootCommand()
	rootCmd.AddCommand(
		NewInitCommand(),
		NewValidateCommand(),
		NewDeployCommand(),
	)

	if err := fang.Execute(context.Background(), rootCmd,
		fang.WithNotifySignal(os.Interrupt),
	); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			os.Exit(ExitTimedOut)
		}

		os.Exit(ExitError)
	}
}

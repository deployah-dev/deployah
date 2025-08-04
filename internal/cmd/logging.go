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

package cmd

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// setupCharmLogger configures the Charm Bracelet logger based on the provided command-line flags.
func setupCharmLogger(cmd *cobra.Command, logLevel string, noColor bool, quiet bool) error {
	// If the "quiet" flag is set, disable all logging
	if quiet {
		// Create a logger that writes to io.Discard
		nullLogger := log.NewWithOptions(io.Discard, log.Options{
			Level: log.FatalLevel, // Only fatal messages will be logged (which won't happen)
		})
		cmd.SetContext(contextWithLogger(cmd.Context(), nullLogger))
		return nil
	}

	// Parse log level
	var level log.Level
	switch logLevel {
	case "trace", "debug":
		level = log.DebugLevel
	case "info":
		level = log.InfoLevel
	case "warn", "warning":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	case "fatal":
		level = log.FatalLevel
	case "panic":
		level = log.FatalLevel // Charm log doesn't have panic level, use fatal
	default:
		level = log.InfoLevel
	}

	// Create logger options
	options := log.Options{
		Level:           level,
		ReportTimestamp: true,
		TimeFormat:      time.RFC3339,
		Prefix:          "🚀 Deployah",
		ReportCaller:    true,
	}

	// Disable colors if requested
	if noColor {
		options.Formatter = log.TextFormatter
	}

	// Create the logger
	logger := log.NewWithOptions(os.Stderr, options)

	// Set custom styles for better visual appeal
	styles := log.DefaultStyles()
	styles.Levels[log.InfoLevel] = styles.Levels[log.InfoLevel].Foreground(lipgloss.Color("#00ff00"))
	styles.Levels[log.WarnLevel] = styles.Levels[log.WarnLevel].Foreground(lipgloss.Color("#ffff00"))
	styles.Levels[log.ErrorLevel] = styles.Levels[log.ErrorLevel].Foreground(lipgloss.Color("#ff0000"))
	styles.Levels[log.FatalLevel] = styles.Levels[log.FatalLevel].Foreground(lipgloss.Color("#ff0000")).Bold(true)

	// Custom styles for common keys
	styles.Keys["env"] = styles.Keys["env"].Foreground(lipgloss.Color("#00ffff"))
	styles.Keys["file"] = styles.Keys["file"].Foreground(lipgloss.Color("#ff00ff"))
	styles.Keys["err"] = styles.Keys["err"].Foreground(lipgloss.Color("#ff0000"))

	logger.SetStyles(styles)

	// Store logger in context
	cmd.SetContext(contextWithLogger(cmd.Context(), logger))

	return nil
}

// GetLogger retrieves the logger from the command context
func GetLogger(cmd *cobra.Command) *log.Logger {
	if logger := cmd.Context().Value(loggerKey{}); logger != nil {
		return logger.(*log.Logger)
	}
	// Fallback to default logger if none is set
	return log.New(os.Stderr)
}

// loggerKey is a context key for storing the logger
type loggerKey struct{}

// contextWithLogger adds a logger to the context
func contextWithLogger(ctx context.Context, logger *log.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

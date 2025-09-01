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

package logging

import (
	"fmt"
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
	"github.com/spf13/cobra"
)

// SetupCharmLogger configures the Charm Bracelet logger based on the provided command-line flags.
func SetupCharmLogger(cmd *cobra.Command, logLevel string, noColor, quiet bool) error {
	// Quiet mode: disable all logging by sending output to io.Discard
	if quiet {
		nullLogger := log.NewWithOptions(io.Discard, log.Options{
			Level: log.FatalLevel,
		})
		cmd.SetContext(WithLogger(cmd.Context(), nullLogger))
		return nil
	}

	// Parse log level string to log.Level
	level, err := log.ParseLevel(logLevel)
	if err != nil {
		return fmt.Errorf("invalid log level: %w", err)
	}

	// Configure logger options
	options := log.Options{
		Level:           level,
		ReportTimestamp: true,
		TimeFormat:      LogTimeFormat,
		Prefix:          LogPrefix,
		ReportCaller:    level == log.DebugLevel, // Only show caller for debug level
	}
	if noColor {
		options.Formatter = log.TextFormatter
	}

	logger := log.NewWithOptions(os.Stderr, options)

	// Enhance log styles for clarity and accessibility
	styles := log.DefaultStyles()
	styles.Levels[log.InfoLevel] = styles.Levels[log.InfoLevel].Foreground(lipgloss.Color("#00ff00"))
	styles.Levels[log.WarnLevel] = styles.Levels[log.WarnLevel].Foreground(lipgloss.Color("#ffff00"))
	styles.Levels[log.ErrorLevel] = styles.Levels[log.ErrorLevel].Foreground(lipgloss.Color("#ff0000"))
	styles.Levels[log.FatalLevel] = styles.Levels[log.FatalLevel].Foreground(lipgloss.Color("#ff0000")).Bold(true)

	// Highlight common keys for better log scanning
	for key, color := range map[string]string{
		"env":  "#00ffff",
		"file": "#ff00ff",
		"err":  "#ff0000",
	} {
		styles.Keys[key] = styles.Keys[key].Foreground(lipgloss.Color(color))
	}

	logger.SetStyles(styles)

	// Create observable logger with metrics collection
	observableLogger := NewObservableLogger(logger)

	// Add metrics collector for basic observability
	metricsCollector := NewMetricsCollector()
	observableLogger.AddHook(metricsCollector)

	// Store observable logger in command context for downstream retrieval
	cmd.SetContext(WithObservableLogger(cmd.Context(), observableLogger))
	return nil
}

// GetLogger retrieves the logger from the command context
func GetLogger(cmd *cobra.Command) *log.Logger {
	if logger := From(cmd.Context()); logger != nil {
		return logger
	}
	// Fallback to default logger if none is set
	return log.New(os.Stderr)
}

// GetObservableLogger retrieves the observable logger from the command context
func GetObservableLogger(cmd *cobra.Command) *ObservableLogger {
	if obsLogger := FromObservable(cmd.Context()); obsLogger != nil {
		return obsLogger
	}
	// Fallback to a basic observable logger
	return NewObservableLogger(log.New(os.Stderr))
}

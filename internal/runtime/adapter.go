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

// Package runtime provides the runtime implementation for the Deployah application.
package runtime

import "github.com/charmbracelet/log"

// LoggerAdapter adapts the charmbracelet log.Logger to implement the LoggerProvider interface for compatibility with the charmbracelet/log.Logger.
type LoggerAdapter struct {
	logger *log.Logger
}

// NewLoggerAdapter creates a new adapter for the LoggerProvider interface for compatibility with the charmbracelet/log.Logger.
func NewLoggerAdapter(logger *log.Logger) LoggerProvider {
	return &LoggerAdapter{logger: logger}
}

// Debug implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) Debug(msg string, keyvals ...interface{}) {
	la.logger.Debug(msg, keyvals...)
}

// Info implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) Info(msg string, keyvals ...interface{}) {
	la.logger.Info(msg, keyvals...)
}

// Warn implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) Warn(msg string, keyvals ...interface{}) {
	la.logger.Warn(msg, keyvals...)
}

// Error implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) Error(msg string, keyvals ...interface{}) {
	la.logger.Error(msg, keyvals...)
}

// Fatal implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) Fatal(msg string, keyvals ...interface{}) {
	la.logger.Fatal(msg, keyvals...)
}

// With implements LoggerProvider for compatibility with the charmbracelet/log.Logger.
func (la *LoggerAdapter) With(keyvals ...interface{}) LoggerProvider {
	return &LoggerAdapter{logger: la.logger.With(keyvals...)}
}

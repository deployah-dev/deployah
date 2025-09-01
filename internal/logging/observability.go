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
	"context"
	"sync"
	"time"

	"github.com/charmbracelet/log"
)

// ObservabilityHook defines an interface for observability hooks that can be
// attached to the logging system to provide metrics, tracing, and monitoring.
type ObservabilityHook interface {
	// OnLog is called whenever a log event occurs
	OnLog(ctx context.Context, level log.Level, msg string, keyvals []interface{})

	// OnError is called whenever an error-level log occurs
	OnError(ctx context.Context, msg string, err error, keyvals []interface{})

	// OnMetric is called to record custom metrics
	OnMetric(ctx context.Context, name string, value float64, tags map[string]string)

	// Close cleans up resources used by the hook
	Close() error
}

// MetricsCollector collects and exports metrics from log events.
type MetricsCollector struct {
	mu      sync.RWMutex
	metrics map[string]*Metric
	hooks   []MetricsHook
}

// Metric represents a collected metric with its metadata.
type Metric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags"`
	Timestamp time.Time         `json:"timestamp"`
	Count     int64             `json:"count"`
}

// MetricsHook defines an interface for metrics exporters.
type MetricsHook interface {
	// Export exports collected metrics to an external system
	Export(ctx context.Context, metrics []*Metric) error

	// Close cleans up resources
	Close() error
}

// NewMetricsCollector creates a new metrics collector.
func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		metrics: make(map[string]*Metric),
		hooks:   make([]MetricsHook, 0),
	}
}

// AddHook adds a metrics export hook.
func (mc *MetricsCollector) AddHook(hook MetricsHook) {
	mc.mu.Lock()
	defer mc.mu.Unlock()
	mc.hooks = append(mc.hooks, hook)
}

// OnLog implements ObservabilityHook.
func (mc *MetricsCollector) OnLog(ctx context.Context, level log.Level, msg string, keyvals []interface{}) {
	// Count log events by level
	mc.recordMetric(ctx, "deployah.logs.count", 1, map[string]string{
		"level": level.String(),
	})
}

// OnError implements ObservabilityHook.
func (mc *MetricsCollector) OnError(ctx context.Context, msg string, err error, keyvals []interface{}) {
	// Count errors by type/category
	tags := map[string]string{
		"error_type": "unknown",
	}

	// Extract error context from keyvals
	for i := 0; i < len(keyvals)-1; i += 2 {
		if key, ok := keyvals[i].(string); ok {
			if value, ok := keyvals[i+1].(string); ok {
				switch key {
				case "component", "operation", "cmd":
					tags[key] = value
				}
			}
		}
	}

	mc.recordMetric(ctx, "deployah.errors.count", 1, tags)
}

// OnMetric implements ObservabilityHook.
func (mc *MetricsCollector) OnMetric(ctx context.Context, name string, value float64, tags map[string]string) {
	mc.recordMetric(ctx, name, value, tags)
}

// recordMetric records a metric with aggregation.
func (mc *MetricsCollector) recordMetric(ctx context.Context, name string, value float64, tags map[string]string) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	// Create metric key from name and tags
	key := mc.buildMetricKey(name, tags)

	now := time.Now()
	if existing, exists := mc.metrics[key]; exists {
		// Aggregate existing metric
		existing.Value += value
		existing.Count++
		existing.Timestamp = now
	} else {
		// Create new metric
		mc.metrics[key] = &Metric{
			Name:      name,
			Value:     value,
			Tags:      copyTags(tags),
			Timestamp: now,
			Count:     1,
		}
	}
}

// buildMetricKey creates a unique key for a metric based on name and tags.
func (mc *MetricsCollector) buildMetricKey(name string, tags map[string]string) string {
	key := name
	for k, v := range tags {
		key += ":" + k + "=" + v
	}
	return key
}

// copyTags creates a copy of the tags map.
func copyTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	copy := make(map[string]string, len(tags))
	for k, v := range tags {
		copy[k] = v
	}
	return copy
}

// ExportMetrics exports all collected metrics to registered hooks.
func (mc *MetricsCollector) ExportMetrics(ctx context.Context) error {
	mc.mu.RLock()
	metrics := make([]*Metric, 0, len(mc.metrics))
	for _, metric := range mc.metrics {
		metrics = append(metrics, metric)
	}
	hooks := make([]MetricsHook, len(mc.hooks))
	copy(hooks, mc.hooks)
	mc.mu.RUnlock()

	// Export to all hooks
	for _, hook := range hooks {
		if err := hook.Export(ctx, metrics); err != nil {
			// Log export error but continue with other hooks
			continue
		}
	}

	return nil
}

// Close implements ObservabilityHook.
func (mc *MetricsCollector) Close() error {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	for _, hook := range mc.hooks {
		hook.Close()
	}

	mc.hooks = nil
	mc.metrics = nil
	return nil
}

// TracingHook provides distributed tracing capabilities.
type TracingHook struct {
	tracer Tracer
}

// Tracer defines an interface for distributed tracing implementations.
type Tracer interface {
	// StartSpan starts a new span with the given name
	StartSpan(ctx context.Context, name string) (context.Context, Span)

	// Close cleans up the tracer
	Close() error
}

// Span represents a tracing span.
type Span interface {
	// SetTag sets a tag on the span
	SetTag(key string, value interface{})

	// SetError marks the span as having an error
	SetError(err error)

	// Finish completes the span
	Finish()
}

// NewTracingHook creates a new tracing hook.
func NewTracingHook(tracer Tracer) *TracingHook {
	return &TracingHook{tracer: tracer}
}

// OnLog implements ObservabilityHook.
func (th *TracingHook) OnLog(ctx context.Context, level log.Level, msg string, keyvals []interface{}) {
	// Create a span for significant log events
	if level >= log.WarnLevel {
		spanCtx, span := th.tracer.StartSpan(ctx, "log."+level.String())
		defer span.Finish()
		_ = spanCtx // Use the span context (in a real implementation you'd pass it down)

		span.SetTag("log.level", level.String())
		span.SetTag("log.message", msg)

		// Add keyvals as tags
		for i := 0; i < len(keyvals)-1; i += 2 {
			if key, ok := keyvals[i].(string); ok {
				span.SetTag("log."+key, keyvals[i+1])
			}
		}
	}
}

// OnError implements ObservabilityHook.
func (th *TracingHook) OnError(ctx context.Context, msg string, err error, keyvals []interface{}) {
	ctx, span := th.tracer.StartSpan(ctx, "error")
	defer span.Finish()

	span.SetError(err)
	span.SetTag("error.message", msg)

	// Add keyvals as tags
	for i := 0; i < len(keyvals)-1; i += 2 {
		if key, ok := keyvals[i].(string); ok {
			span.SetTag("error."+key, keyvals[i+1])
		}
	}
}

// OnMetric implements ObservabilityHook.
func (th *TracingHook) OnMetric(ctx context.Context, name string, value float64, tags map[string]string) {
	// Metrics can be optionally traced for debugging
	ctx, span := th.tracer.StartSpan(ctx, "metric."+name)
	defer span.Finish()

	span.SetTag("metric.name", name)
	span.SetTag("metric.value", value)

	for k, v := range tags {
		span.SetTag("metric."+k, v)
	}
}

// Close implements ObservabilityHook.
func (th *TracingHook) Close() error {
	if th.tracer != nil {
		return th.tracer.Close()
	}
	return nil
}

// ObservableLogger wraps a logger with observability hooks.
type ObservableLogger struct {
	logger *log.Logger
	hooks  []ObservabilityHook
	mu     sync.RWMutex
}

// NewObservableLogger creates a new observable logger.
func NewObservableLogger(logger *log.Logger) *ObservableLogger {
	return &ObservableLogger{
		logger: logger,
		hooks:  make([]ObservabilityHook, 0),
	}
}

// AddHook adds an observability hook.
func (ol *ObservableLogger) AddHook(hook ObservabilityHook) {
	ol.mu.Lock()
	defer ol.mu.Unlock()
	ol.hooks = append(ol.hooks, hook)
}

// Debug logs a debug message and notifies hooks.
func (ol *ObservableLogger) Debug(msg string, keyvals ...interface{}) {
	ol.logger.Debug(msg, keyvals...)
	ol.notifyHooks(context.Background(), log.DebugLevel, msg, keyvals)
}

// Info logs an info message and notifies hooks.
func (ol *ObservableLogger) Info(msg string, keyvals ...interface{}) {
	ol.logger.Info(msg, keyvals...)
	ol.notifyHooks(context.Background(), log.InfoLevel, msg, keyvals)
}

// Warn logs a warning message and notifies hooks.
func (ol *ObservableLogger) Warn(msg string, keyvals ...interface{}) {
	ol.logger.Warn(msg, keyvals...)
	ol.notifyHooks(context.Background(), log.WarnLevel, msg, keyvals)
}

// Error logs an error message and notifies hooks.
func (ol *ObservableLogger) Error(msg string, keyvals ...interface{}) {
	ol.logger.Error(msg, keyvals...)
	ol.notifyHooks(context.Background(), log.ErrorLevel, msg, keyvals)

	// Special handling for errors
	var err error
	for i := 0; i < len(keyvals)-1; i += 2 {
		if key, ok := keyvals[i].(string); ok && key == "err" {
			if e, ok := keyvals[i+1].(error); ok {
				err = e
				break
			}
		}
	}

	ol.notifyErrorHooks(context.Background(), msg, err, keyvals)
}

// Fatal logs a fatal message and notifies hooks.
func (ol *ObservableLogger) Fatal(msg string, keyvals ...interface{}) {
	ol.logger.Fatal(msg, keyvals...)
	ol.notifyHooks(context.Background(), log.FatalLevel, msg, keyvals)
}

// With returns a new logger with additional key-value pairs.
func (ol *ObservableLogger) With(keyvals ...interface{}) *ObservableLogger {
	return &ObservableLogger{
		logger: ol.logger.With(keyvals...),
		hooks:  ol.hooks, // Share hooks with the parent logger
	}
}

// Metric records a custom metric.
func (ol *ObservableLogger) Metric(ctx context.Context, name string, value float64, tags map[string]string) {
	ol.notifyMetricHooks(ctx, name, value, tags)
}

// notifyHooks notifies all registered hooks about a log event.
func (ol *ObservableLogger) notifyHooks(ctx context.Context, level log.Level, msg string, keyvals []interface{}) {
	ol.mu.RLock()
	hooks := make([]ObservabilityHook, len(ol.hooks))
	copy(hooks, ol.hooks)
	ol.mu.RUnlock()

	for _, hook := range hooks {
		hook.OnLog(ctx, level, msg, keyvals)
	}
}

// notifyErrorHooks notifies all registered hooks about an error event.
func (ol *ObservableLogger) notifyErrorHooks(ctx context.Context, msg string, err error, keyvals []interface{}) {
	ol.mu.RLock()
	hooks := make([]ObservabilityHook, len(ol.hooks))
	copy(hooks, ol.hooks)
	ol.mu.RUnlock()

	for _, hook := range hooks {
		hook.OnError(ctx, msg, err, keyvals)
	}
}

// notifyMetricHooks notifies all registered hooks about a metric event.
func (ol *ObservableLogger) notifyMetricHooks(ctx context.Context, name string, value float64, tags map[string]string) {
	ol.mu.RLock()
	hooks := make([]ObservabilityHook, len(ol.hooks))
	copy(hooks, ol.hooks)
	ol.mu.RUnlock()

	for _, hook := range hooks {
		hook.OnMetric(ctx, name, value, tags)
	}
}

// Close closes all observability hooks.
func (ol *ObservableLogger) Close() error {
	ol.mu.Lock()
	defer ol.mu.Unlock()

	for _, hook := range ol.hooks {
		hook.Close()
	}

	ol.hooks = nil
	return nil
}

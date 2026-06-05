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

package localkube

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	kindlog "sigs.k8s.io/kind/pkg/log"
)

// eventSink is a thread-safe, swappable event dispatch target used by
// kindProvider to route Kind phase events to the active per-call handler
// during create/delete, falling back to the manager-level default otherwise.
type eventSink struct {
	mu  sync.Mutex
	fn  func(Event) // current target; always non-nil after construction
	def func(Event) // the permanent manager-level fallback
}

// emit dispatches ev to the current target. Safe for concurrent use.
func (s *eventSink) emit(ev Event) {
	s.mu.Lock()
	fn := s.fn
	s.mu.Unlock()
	fn(ev)
}

// set replaces the current target for the duration of an operation.
func (s *eventSink) set(fn func(Event)) {
	s.mu.Lock()
	s.fn = fn
	s.mu.Unlock()
}

// reset restores the manager-level default.
func (s *eventSink) reset() {
	s.mu.Lock()
	s.fn = s.def
	s.mu.Unlock()
}

// slogAdapter bridges Kind's [log.Logger] interface to a [*slog.Logger].
// Kind's V(0) progress messages (status lines with •/✓/✗ prefixes) are
// intercepted and emitted as [Event] values through onEvent; unrecognized
// status lines are forwarded to slog at Debug level.
// Warn/Error go to slog directly; V(1+) goes to slog at Debug level.
type slogAdapter struct {
	l       *slog.Logger
	onEvent func(Event)
}

var _ kindlog.Logger = (*slogAdapter)(nil)

func (a *slogAdapter) Warn(msg string)              { a.l.Warn(msg) }
func (a *slogAdapter) Warnf(f string, args ...any)  { a.l.Warn(fmt.Sprintf(f, args...)) }
func (a *slogAdapter) Error(msg string)             { a.l.Error(msg) }
func (a *slogAdapter) Errorf(f string, args ...any) { a.l.Error(fmt.Sprintf(f, args...)) }
func (a *slogAdapter) V(level kindlog.Level) kindlog.InfoLogger {
	return &slogInfoAdapter{l: a.l, level: int(level), onEvent: a.onEvent}
}

// slogInfoAdapter implements kind's InfoLogger for a specific verbosity level.
// V(0) messages matching Kind's status format are converted to Events;
// unrecognized status lines and all other messages are forwarded to slog.
type slogInfoAdapter struct {
	l       *slog.Logger
	level   int
	onEvent func(Event)
}

var _ kindlog.InfoLogger = (*slogInfoAdapter)(nil)

func (a *slogInfoAdapter) Info(msg string) {
	if a.level == 0 {
		if ev, ok := parseKindStatus(msg); ok {
			a.onEvent(ev)
			return
		}
		// Non-status or unrecognized — forward to slog at Debug.
		a.l.LogAttrs(context.Background(), slog.LevelDebug, msg)
		return
	}
	a.l.LogAttrs(context.Background(), slog.LevelDebug, msg,
		slog.Int("verbosity", a.level))
}

func (a *slogInfoAdapter) Infof(f string, args ...any) {
	a.Info(fmt.Sprintf(f, args...))
}

// Enabled returns true because slog handles its own level filtering.
func (a *slogInfoAdapter) Enabled() bool { return true }

// parseKindStatus detects Kind's status lines and converts them to Events
// with a normalized, closed [Step] constant. Unrecognized phase strings are
// returned as (Event{}, false) so the caller can forward them to [slog.Debug].
//
// Kind emits: " • <phase>  ...\n" (started), " ✓ <phase>\n" (completed),
// " ✗ <phase>\n" (failed).
func parseKindStatus(msg string) (Event, bool) {
	msg = strings.TrimSpace(msg)
	var status StepStatus
	var phase string
	switch {
	case strings.HasPrefix(msg, "•"):
		status = StepStarted
		phase = strings.TrimSpace(strings.TrimPrefix(msg, "•"))
		phase = strings.TrimSuffix(phase, "...")
		phase = strings.TrimSpace(phase)
	case strings.HasPrefix(msg, "✓"):
		status = StepCompleted
		phase = strings.TrimSpace(strings.TrimPrefix(msg, "✓"))
	case strings.HasPrefix(msg, "✗"):
		status = StepFailed
		phase = strings.TrimSpace(strings.TrimPrefix(msg, "✗"))
	default:
		return Event{}, false
	}

	phase = stripEmoji(phase)
	step, known := normalizeKindPhase(phase)
	if !known {
		return Event{}, false
	}
	return Event{Step: step, Status: status}, true
}

// normalizeKindPhase maps a stripped Kind phase string to a closed [Step]
// constant. Returns ("", false) for any phase not in the known set; these
// are routed to [slog.Debug] instead of the event stream.
func normalizeKindPhase(phase string) (Step, bool) {
	switch phase {
	case "Creating cluster":
		return StepCreating, true
	case "Ensuring node image", "Pulling node image":
		return StepPullingNodeImage, true
	case "Preparing nodes":
		return StepPreparingNodes, true
	case "Writing configuration":
		return StepWritingConfiguration, true
	case "Starting control-plane":
		return StepStartingControlPlane, true
	case "Installing CNI":
		return StepInstallingCNI, true
	case "Installing StorageClass":
		return StepInstallingStorageClass, true
	case "Joining more control-plane nodes":
		return StepJoiningControlPlane, true
	case "Joining worker nodes":
		return StepJoiningWorkers, true
	case "Waiting for the control plane to be ready":
		return StepWaitingForReady, true
	default:
		return "", false
	}
}

// stripEmoji removes trailing emoji, variation selectors, and ZWJs from Kind
// status text. It targets explicit Unicode blocks rather than stripping every
// rune ≥ U+2000, which would incorrectly discard CJK characters.
func stripEmoji(s string) string {
	s = strings.TrimSpace(s)
	rs := []rune(s)
	end := len(rs)
	for end > 0 {
		r := rs[end-1]
		if isEmojiRune(r) {
			end--
			continue
		}
		break
	}
	return strings.TrimSpace(string(rs[:end]))
}

func isEmojiRune(r rune) bool {
	switch {
	case r == 0xFE0F:
		return true
	case r == 0x200D:
		return true
	case r >= 0x2600 && r <= 0x27BF:
		return true
	case r >= 0x1F000 && r <= 0x1FFFF:
		return true
	default:
		return false
	}
}

// Copyright 2026 The Deployah Authors
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

package e2e

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultPhaseTimeout is the budget for one cleanup phase.
const DefaultPhaseTimeout = 2 * time.Minute

// CleanupPhase is one cleanup action with its own deadline.
type CleanupPhase struct {
	Name string
	Run  func(context.Context) error
}

// CleanupOption configures [RunCleanup].
type CleanupOption func(*cleanupConfig)

type cleanupConfig struct {
	phaseTimeout time.Duration
	budget       time.Duration
}

// WithPhaseTimeout sets how long each phase may run.
func WithPhaseTimeout(d time.Duration) CleanupOption {
	return func(cfg *cleanupConfig) { cfg.phaseTimeout = d }
}

// WithBudget sets the outer cleanup deadline explicitly.
func WithBudget(d time.Duration) CleanupOption {
	return func(cfg *cleanupConfig) { cfg.budget = d }
}

// RunCleanup runs phases under one overall deadline.
func RunCleanup(ctx context.Context, phases []CleanupPhase, opts ...CleanupOption) error {
	// Each phase takes its own timeout from ctx, so one slow phase cannot
	// eat the time of the next. A lost or uncertain namespace claim stops
	// the rest. Other errors are kept, and the next phase still runs.
	// Pass a live context. The Kind suite uses a fresh one because cleanup
	// runs after the test context is canceled.
	if ctx == nil {
		return errors.New("cleanup context is nil")
	}
	cfg := cleanupConfig{phaseTimeout: DefaultPhaseTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.phaseTimeout <= 0 {
		return errors.New("cleanup phase timeout must be positive")
	}
	if len(phases) == 0 {
		return nil
	}
	budget := cfg.budget
	if budget <= 0 {
		budget = cfg.phaseTimeout * time.Duration(len(phases))
	}
	outer, cancelOuter := context.WithTimeout(ctx, budget)
	defer cancelOuter()

	var errs []error
	for _, phase := range phases {
		if outer.Err() != nil {
			errs = append(errs, fmt.Errorf("%s: %w", phase.Name, context.DeadlineExceeded))
			break
		}
		err := runPhase(outer, cfg.phaseTimeout, phase)
		if err == nil {
			continue
		}
		errs = append(errs, err)
		if errors.Is(err, ErrNotOwner) || errors.Is(err, ErrOwnershipUncertain) {
			break
		}
	}
	return errors.Join(errs...)
}

func runPhase(parent context.Context, timeout time.Duration, phase CleanupPhase) error {
	phaseCtx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if phase.Run == nil {
		return fmt.Errorf("%s: nil cleanup function", phase.Name)
	}
	if err := phase.Run(phaseCtx); err != nil {
		return fmt.Errorf("%s: %w", phase.Name, err)
	}
	return nil
}

// Package ui provides UI components for interactive flows
package ui

import "fmt"

// ProgressTracker helps track steps during interactive flows
type ProgressTracker struct {
	currentStep int
	totalSteps  int
	steps       []string
}

// NewProgressTracker creates a new progress tracker with the default steps
func NewProgressTracker() *ProgressTracker {
	return &ProgressTracker{
		currentStep: 0,
		totalSteps:  4,
		steps: []string{
			"Project Name",
			"Environments",
			"Components",
			"Summary",
		},
	}
}

// NextStep increments the current step
func (pt *ProgressTracker) NextStep() { pt.currentStep++ }

// GetCurrentStep returns the current step
func (pt *ProgressTracker) GetCurrentStep() string {
	if pt.currentStep >= len(pt.steps) {
		return "Complete"
	}
	return fmt.Sprintf("Step %d/%d: %s", pt.currentStep+1, pt.totalSteps, pt.steps[pt.currentStep])
}

package action

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/k8s"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ReleaseGetter lists Helm releases that match a label selector.
type ReleaseGetter interface {
	ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error)
}

// StatusParams holds the parameters for a status query.
type StatusParams struct {
	// Project is the Deployah project name.
	Project string
	// Environment limits status to one environment.
	Environment string
}

// Status encapsulates the status business logic.
type Status struct {
	getter ReleaseGetter
}

// NewStatus constructs a Status with the given release getter.
func NewStatus(getter ReleaseGetter) *Status {
	return &Status{getter: getter}
}

// Run returns sorted releases for the given project.
// Returns an error if none found.
func (s *Status) Run(ctx context.Context, params StatusParams) ([]*v1.Release, error) {
	selector, err := k8s.BuildLabelSelector(params.Project, params.Environment)
	if err != nil {
		return nil, fmt.Errorf("build selector: %w", err)
	}

	releases, err := s.getter.ListReleases(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	if len(releases) == 0 {
		msg := fmt.Sprintf("no releases found for project '%s'", params.Project)
		if params.Environment != "" {
			msg += fmt.Sprintf(" in environment '%s'", params.Environment)
		}
		return nil, errors.New(msg)
	}

	sort.Slice(releases, func(i, j int) bool {
		return releases[i].Name < releases[j].Name
	})
	return releases, nil
}

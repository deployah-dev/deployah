package action

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/labels"

	"deployah.dev/deployah/internal/k8s"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ReleaseLister abstracts Helm list operations.
type ReleaseLister interface {
	ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error)
}

// ListParams holds the filter parameters for listing releases.
type ListParams struct {
	// Project limits results to one Deployah project.
	Project string
	// Environment limits results to one environment.
	Environment string
}

// List encapsulates the list business logic.
type List struct {
	lister ReleaseLister
}

// NewList constructs a List with the given release lister.
func NewList(lister ReleaseLister) *List {
	return &List{lister: lister}
}

// Run returns non-nil releases matching the filters.
// Returns an empty slice (not an error) if none found.
func (l *List) Run(ctx context.Context, params ListParams) ([]*v1.Release, error) {
	if params.Environment != "" {
		if err := spec.ValidateRequestedEnv(params.Environment); err != nil {
			return nil, err
		}
	}
	selector, err := k8s.BuildLabelSelector(params.Project, params.Environment)
	if err != nil {
		return nil, fmt.Errorf("build selector: %w", err)
	}

	releases, err := l.lister.ListReleases(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	return matchingReleases(releases, params.Project, params.Environment), nil
}

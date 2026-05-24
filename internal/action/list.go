package action

import (
	"context"
	"fmt"

	"deployah.dev/deployah/internal/k8s"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// ReleaseLister abstracts Helm list operations.
type ReleaseLister interface {
	ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error)
}

// ListParams holds the filter parameters for listing releases.
type ListParams struct {
	Project     string
	Environment string
}

// List encapsulates the list business logic.
type List struct {
	lister ReleaseLister
}

func NewList(lister ReleaseLister) *List {
	return &List{lister: lister}
}

// Run returns non-nil releases matching the filters. Returns an empty slice (not an error) if none found.
func (l *List) Run(ctx context.Context, params ListParams) ([]*v1.Release, error) {
	selector, err := k8s.BuildLabelSelector(params.Project, params.Environment)
	if err != nil {
		return nil, fmt.Errorf("build selector: %w", err)
	}

	releases, err := l.lister.ListReleases(ctx, selector)
	if err != nil {
		return nil, fmt.Errorf("list releases: %w", err)
	}

	valid := make([]*v1.Release, 0, len(releases))
	for _, r := range releases {
		if r != nil {
			valid = append(valid, r)
		}
	}
	return valid, nil
}

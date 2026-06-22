package action

import (
	"context"
	"errors"
	"fmt"

	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// ReleaseDeleter abstracts Helm get/delete operations.
type ReleaseDeleter interface {
	GetRelease(ctx context.Context, project, environment string) (*v1.Release, error)
	DeleteRelease(ctx context.Context, project, environment string, wait bool) error
}

// DeleteParams holds the parameters for a delete operation.
type DeleteParams struct {
	// Project is the Deployah project name.
	Project string
	// Environment is the target environment name.
	Environment string
	// Yes skips the not-found check and proceeds with deletion.
	Yes bool
	// DryRun previews deletion without mutating the cluster.
	DryRun bool
	// Wait blocks until all Kubernetes resources are fully removed.
	Wait bool
}

// DeleteResult contains the outcome of a delete check.
type DeleteResult struct {
	// Release is the Helm release when one was found.
	Release *v1.Release
	// NotFound is true when no release exists for the project and environment.
	NotFound bool
}

// Delete encapsulates the delete business logic.
type Delete struct {
	deleter ReleaseDeleter
}

// NewDelete constructs a Delete with the given release deleter.
func NewDelete(deleter ReleaseDeleter) *Delete {
	return &Delete{deleter: deleter}
}

// Check verifies the release exists and returns it. Does not perform deletion.
func (d *Delete) Check(ctx context.Context, params DeleteParams) (*DeleteResult, error) {
	release, err := d.deleter.GetRelease(ctx, params.Project, params.Environment)
	if err != nil {
		if errors.Is(err, helm.ErrReleaseNotFound) {
			if !params.Yes {
				return nil, fmt.Errorf("project '%s' in environment '%s': %w", params.Project, params.Environment, helm.ErrReleaseNotFound)
			}
			return &DeleteResult{NotFound: true}, nil
		}
		return nil, fmt.Errorf("check project status: %w", err)
	}
	return &DeleteResult{Release: release}, nil
}

// Execute performs the actual deletion.
func (d *Delete) Execute(ctx context.Context, project, environment string, wait bool) error {
	if err := d.deleter.DeleteRelease(ctx, project, environment, wait); err != nil {
		return fmt.Errorf("delete release: %w", err)
	}
	return nil
}

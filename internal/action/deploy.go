package action

import (
	"context"
	"fmt"

	"github.com/deployah-dev/deployah/internal/manifest"
)

// Deployer abstracts Helm install/upgrade operations.
type Deployer interface {
	InstallApp(ctx context.Context, m *manifest.Manifest, environment string, dryRun bool) error
}

// ManifestLoader loads and validates a manifest for an environment.
type ManifestLoader interface {
	Manifest(ctx context.Context, environment string) (*manifest.Manifest, error)
}

// Deploy encapsulates the deploy business logic.
type Deploy struct {
	deployer Deployer
	loader   ManifestLoader
}

func NewDeploy(deployer Deployer, loader ManifestLoader) *Deploy {
	return &Deploy{deployer: deployer, loader: loader}
}

func (d *Deploy) Run(ctx context.Context, environment string, dryRun bool) (*manifest.Manifest, error) {
	m, err := d.loader.Manifest(ctx, environment)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	if err := d.deployer.InstallApp(ctx, m, environment, dryRun); err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	return m, nil
}

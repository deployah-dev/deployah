package action

import (
	"context"
	"fmt"

	"deployah.dev/deployah/internal/spec"
)

// Deployer abstracts Helm install/upgrade operations.
type Deployer interface {
	InstallApp(ctx context.Context, m *spec.Spec, environment string, dryRun bool) error
}

// SpecLoader loads and validates a spec for an environment.
type SpecLoader interface {
	Spec(ctx context.Context, environment string) (*spec.Spec, error)
}

// Deploy encapsulates the deploy business logic.
type Deploy struct {
	deployer Deployer
	loader   SpecLoader
}

// NewDeploy constructs a Deploy with the given deployer and loader.
func NewDeploy(deployer Deployer, loader SpecLoader) *Deploy {
	return &Deploy{deployer: deployer, loader: loader}
}

// Run loads the spec and installs or upgrades the Helm release.
func (d *Deploy) Run(ctx context.Context, environment string, dryRun bool) (*spec.Spec, error) {
	m, err := d.loader.Spec(ctx, environment)
	if err != nil {
		return nil, fmt.Errorf("load spec: %w", err)
	}
	if err = d.deployer.InstallApp(ctx, m, environment, dryRun); err != nil {
		return nil, fmt.Errorf("install: %w", err)
	}
	return m, nil
}

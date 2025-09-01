package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/deployah-dev/deployah/internal/manifest"
	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	v1 "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

type Client struct {
	settings      *cli.EnvSettings
	config        *action.Configuration
	timeout       time.Duration
	namespace     string
	kubeconfig    string
	storageDriver string
	debug         bool
}

// Option is a functional option for configuring the Helm client
type Option func(*Client)

// WithNamespace sets the Kubernetes namespace for Helm operations
func WithNamespace(namespace string) Option {
	return func(c *Client) {
		c.namespace = namespace
	}
}

// WithKubeconfig sets the path to the kubeconfig file
func WithKubeconfig(kubeconfig string) Option {
	return func(c *Client) {
		c.kubeconfig = kubeconfig
	}
}

// WithStorageDriver sets the Helm storage driver (secret, configmap, or memory)
func WithStorageDriver(driver string) Option {
	return func(c *Client) {
		c.storageDriver = driver
	}
}

// WithTimeout sets the default timeout for Helm operations
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithDebug controls whether to keep temporary chart directories.
func WithDebug(keep bool) Option {
	return func(c *Client) {
		c.debug = keep
	}
}

// NewClient initializes Helm action configuration with functional options.
// Default storage driver is "secret" if not specified.
// Default timeout is 5 minutes if not specified.
func NewClient(opts ...Option) (*Client, error) {
	// Set defaults
	c := &Client{
		storageDriver: "secret",
		timeout:       5 * time.Minute,
	}

	// Apply all functional options
	for _, opt := range opts {
		opt(c)
	}

	settings := cli.New()

	// Apply configuration overrides
	if c.kubeconfig != "" {
		settings.KubeConfig = c.kubeconfig
	}
	if c.namespace != "" {
		settings.SetNamespace(c.namespace)
	}

	// Validate storage driver
	validDrivers := map[string]bool{"secret": true, "configmap": true, "memory": true}
	if !validDrivers[c.storageDriver] {
		return nil, fmt.Errorf("invalid storage driver '%s': must be one of 'secret', 'configmap', or 'memory'", c.storageDriver)
	}

	c.config = new(action.Configuration)
	if err := c.config.Init(settings.RESTClientGetter(), settings.Namespace(), c.storageDriver); err != nil {
		return nil, fmt.Errorf("failed to initialize Helm configuration: %w", err)
	}

	c.settings = settings

	return c, nil
}

// InstallApp installs or upgrades the app using the embedded chart.
func (c *Client) InstallApp(ctx context.Context, manifest *manifest.Manifest, environment string, dryRun bool) error {
	// Set comprehensive labels for better filtering
	labels := map[string]string{
		"deployah.dev/project":     manifest.Project,
		"deployah.dev/environment": environment,
		"deployah.dev/managed-by":  "deployah",
		"deployah.dev/version":     manifest.ApiVersion,
	}

	// Resolve chart path
	chartPath, err := PrepareChart(ctx, manifest, environment)
	if err != nil {
		return fmt.Errorf("failed to prepare chart: %w", err)
	}

	// Cleanup temp directory unless explicitly kept
	if !c.debug {
		defer os.RemoveAll(chartPath)
	}

	// Values are empty for now, but will be populated later
	values := map[string]any{}

	// Load chart
	ch, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	// For dry-run, always treat as install to avoid cluster connectivity issues
	if dryRun {
		install := action.NewInstall(c.config)
		install.ReleaseName = GenerateReleaseName(manifest.Project, environment)
		install.Namespace = c.settings.Namespace()
		install.CreateNamespace = true
		install.Timeout = c.timeout
		install.WaitStrategy = kube.StatusWatcherStrategy
		install.RollbackOnFailure = true
		install.DryRun = true
		install.ClientOnly = true
		install.DisableHooks = true
		install.DisableOpenAPIValidation = true
		install.Labels = labels

		if _, err := install.RunWithContext(ctx, ch, values); err != nil {
			return c.wrapHelmError("install", GenerateReleaseName(manifest.Project, environment), err)
		}
		return nil
	}

	// Decide install vs upgrade by checking release history
	history := action.NewHistory(c.config)
	history.Max = 1
	_, histErr := history.Run(GenerateReleaseName(manifest.Project, environment))

	if histErr != nil {
		// Not found -> install. For other errors, proceed with install attempt as well
		// Fresh install
		install := action.NewInstall(c.config)
		install.ReleaseName = GenerateReleaseName(manifest.Project, environment)
		install.Namespace = c.settings.Namespace()
		install.CreateNamespace = true
		install.Timeout = c.timeout
		install.WaitStrategy = kube.StatusWatcherStrategy
		install.RollbackOnFailure = true
		install.Labels = labels

		if _, err := install.RunWithContext(ctx, ch, values); err != nil {
			return c.wrapHelmError("install", GenerateReleaseName(manifest.Project, environment), err)
		}
		return nil
	}

	// Upgrade existing release
	upgrade := action.NewUpgrade(c.config)
	upgrade.Namespace = c.settings.Namespace()
	upgrade.Timeout = c.timeout
	upgrade.RollbackOnFailure = true
	upgrade.WaitStrategy = kube.StatusWatcherStrategy
	upgrade.Labels = labels
	_, err = upgrade.RunWithContext(ctx, GenerateReleaseName(manifest.Project, environment), ch, values)
	if err != nil {
		return c.wrapHelmError("upgrade", GenerateReleaseName(manifest.Project, environment), err)
	}
	return nil
}

// ListReleases returns detailed information about releases in the current namespace.
func (c *Client) ListReleases(ctx context.Context, selector labels.Selector) ([]*v1.Release, error) {
	req, err := labels.NewRequirement("deployah.dev/managed-by", selection.Equals, []string{"deployah"})
	if err != nil {
		return nil, fmt.Errorf("failed to create managed-by label requirement: %w", err)
	}
	selector = selector.Add(*req)

	lister := action.NewList(c.config)
	lister.All = false
	lister.AllNamespaces = false
	lister.Selector = selector.String()
	lister.StateMask = action.ListAll

	rels, err := lister.Run()
	if err != nil {
		return nil, c.wrapHelmError("list", "", err)
	}
	return rels, nil
}

func (c *Client) GetRelease(ctx context.Context, project, environment string) (*v1.Release, error) {
	releaseName := GenerateReleaseName(project, environment)
	get := action.NewGet(c.config)
	get.Version = 0
	release, err := get.Run(releaseName)
	if err != nil {
		return nil, c.wrapHelmError("get", releaseName, err)
	}
	return release, nil
}

// DeleteRelease uninstalls the given release.
func (c *Client) DeleteRelease(ctx context.Context, project, environment string) error {
	if project == "" || environment == "" {
		return errors.New("project or environment cannot be empty")
	}

	releaseName := GenerateReleaseName(project, environment)
	un := action.NewUninstall(c.config)
	un.IgnoreNotFound = true
	un.KeepHistory = false
	un.Timeout = c.timeout
	un.WaitStrategy = kube.StatusWatcherStrategy

	if _, err := un.Run(releaseName); err != nil {
		return c.wrapHelmError("uninstall", releaseName, err)
	}

	return nil
}

// GetReleaseHistory returns the history of a specific release.
func (c *Client) GetReleaseHistory(ctx context.Context, project, environment string) ([]*v1.Release, error) {
	if project == "" || environment == "" {
		return nil, errors.New("project or environment cannot be empty")
	}

	releaseName := GenerateReleaseName(project, environment)

	history := action.NewHistory(c.config)
	history.Max = 10 // Get last 10 revisions
	releases, err := history.Run(releaseName)
	if err != nil {
		return nil, c.wrapHelmError("history", releaseName, err)
	}
	return releases, nil
}

// RollbackRelease rolls back a release to a previous revision.
func (c *Client) RollbackRelease(ctx context.Context, releaseName string, revision int, timeout time.Duration) error {
	if releaseName == "" {
		return errors.New("release name cannot be empty")
	}

	rollback := action.NewRollback(c.config)
	rollback.Version = revision
	rollback.Timeout = timeout
	rollback.WaitStrategy = kube.StatusWatcherStrategy

	if err := rollback.Run(releaseName); err != nil {
		return c.wrapHelmError("rollback", releaseName, err)
	}
	return nil
}

// wrapHelmError provides better error messages for common Helm errors.
func (c *Client) wrapHelmError(operation, releaseName string, err error) error {
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("release '%s' not found", releaseName)
	}
	errMsg := err.Error()

	// Check for common error patterns and provide helpful messages
	switch {
	case strings.Contains(errMsg, "not found"):
		return fmt.Errorf("release '%s' not found", releaseName)
	case strings.Contains(errMsg, "another operation") || strings.Contains(errMsg, "pending"):
		return fmt.Errorf("another operation is in progress for release '%s', please try again later", releaseName)
	case strings.Contains(errMsg, "timeout"):
		return fmt.Errorf("operation timed out for release '%s': %w", releaseName, err)
	case strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "dial"):
		return fmt.Errorf("unable to connect to Kubernetes cluster: %w", err)
	case strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "unauthorized"):
		return fmt.Errorf("insufficient permissions for %s operation on release '%s': %w", operation, releaseName, err)
	case strings.Contains(errMsg, "already exists"):
		return fmt.Errorf("release '%s' already exists", releaseName)
	default:
		return fmt.Errorf("helm %s failed for release '%s': %w", operation, releaseName, err)
	}
}

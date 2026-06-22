package helm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"helm.sh/helm/v4/pkg/action"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/cli"
	"helm.sh/helm/v4/pkg/kube"
	"helm.sh/helm/v4/pkg/release"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"

	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

var (
	// ErrClusterUnreachable is returned when the Kubernetes cluster cannot be reached.
	ErrClusterUnreachable = errors.New("kubernetes cluster unreachable")
	// ErrReleaseNotFound is returned when a Helm release does not exist.
	ErrReleaseNotFound = errors.New("release not found")
	// ErrReleaseAlreadyExists is returned when a Helm release already exists.
	ErrReleaseAlreadyExists = errors.New("release already exists")
	// ErrReleasePending is returned when a Helm release has an operation in progress.
	ErrReleasePending = errors.New("another operation is in progress")
)

// Client wraps Helm action configuration for Deployah operations.
type Client struct {
	settings             *cli.EnvSettings
	config               *action.Configuration
	timeout              time.Duration
	namespace            string
	kubeconfig           string
	kubeContext          string
	extraKubeconfigPaths []string
	storageDriver        string
	debug                bool
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

// WithKubeContext sets the Kubernetes context to use, overriding the
// kubeconfig's current context.
func WithKubeContext(kubeContext string) Option {
	return func(c *Client) {
		c.kubeContext = kubeContext
	}
}

// WithExtraKubeconfigPaths appends additional kubeconfig file paths so their
// contexts are available alongside the default kubeconfig. This is ignored
// when WithKubeconfig is also set, because an explicit path takes full
// precedence and makes extra paths redundant.
func WithExtraKubeconfigPaths(paths ...string) Option {
	return func(c *Client) {
		c.extraKubeconfigPaths = append(c.extraKubeconfigPaths, paths...)
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

	// An explicit kubeconfig takes full precedence; extra paths are ignored,
	// matching client-go's ExplicitPath semantics. Otherwise prepend the extra
	// paths before the default kubeconfig so deployah-managed contexts (e.g.
	// a recreated Kind cluster) take priority over stale entries in
	// ~/.kube/config.
	if c.kubeconfig != "" {
		settings.KubeConfig = c.kubeconfig
	} else if len(c.extraKubeconfigPaths) > 0 {
		all := make([]string, 0, len(c.extraKubeconfigPaths)+1)
		all = append(all, c.extraKubeconfigPaths...)
		all = append(all, settings.KubeConfig)
		var parts []string
		for _, p := range all {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 0 {
			settings.KubeConfig = strings.Join(parts, string(os.PathListSeparator))
		}
	}
	if c.kubeContext != "" {
		settings.KubeContext = c.kubeContext
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

// IsReachable reports whether the configured Kubernetes cluster is reachable.
//
// NOTE: this is also a workaround for helm/helm#32183. Helm v4.2.0 panics on
// the second IsReachable call when the first one fails (typed-nil cached in
// getKubeClient). Calling this once before InstallApp prevents InstallApp from
// ever hitting the second call against a poisoned client.
func (c *Client) IsReachable() error {
	if err := c.config.KubeClient.IsReachable(); err != nil {
		return fmt.Errorf("%w: %w", ErrClusterUnreachable, err)
	}
	return nil
}

// InstallApp installs or upgrades the app using the embedded chart.
func (c *Client) InstallApp(ctx context.Context, manifest *spec.Spec, environment string, dryRun bool) error {
	// Set comprehensive labels for better filtering
	labels := map[string]string{
		"deployah.dev/project":     manifest.Project,
		"deployah.dev/environment": environment,
		"deployah.dev/managed-by":  "deployah",
		"deployah.dev/version":     manifest.APIVersion,
	}

	// Resolve chart path
	chartPath, err := PrepareChart(ctx, manifest, environment)
	if err != nil {
		return fmt.Errorf("failed to prepare chart: %w", err)
	}

	// Cleanup temp directory unless explicitly kept
	if !c.debug {
		defer func() {
			if removeErr := os.RemoveAll(chartPath); removeErr != nil {
				fmt.Printf("Warning: failed to cleanup chart temp dir %s: %v\n", chartPath, removeErr)
			}
		}()
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
		install.DryRunStrategy = action.DryRunClient
		install.DisableHooks = true
		install.DisableOpenAPIValidation = true
		install.Labels = labels

		if _, runErr := install.RunWithContext(ctx, ch, values); runErr != nil {
			return c.wrapHelmError("install", GenerateReleaseName(manifest.Project, environment), runErr)
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

		if _, runErr := install.RunWithContext(ctx, ch, values); runErr != nil {
			return c.wrapHelmError("install", GenerateReleaseName(manifest.Project, environment), runErr)
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

// ListReleases returns release details in the current namespace.
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
	return releaserListToV1(rels)
}

// GetRelease retrieves a release by project and environment.
func (c *Client) GetRelease(ctx context.Context, project, environment string) (*v1.Release, error) {
	releaseName := GenerateReleaseName(project, environment)
	get := action.NewGet(c.config)
	get.Version = 0
	rel, err := get.Run(releaseName)
	if err != nil {
		return nil, c.wrapHelmError("get", releaseName, err)
	}
	return releaserToV1(rel)
}

// DeleteRelease uninstalls the given release. When wait is false (the default)
// it returns immediately after hooks complete, matching vanilla `helm uninstall`
// behavior. When wait is true it blocks until all Kubernetes resources are
// fully removed, using the legacy polling strategy (kube.LegacyStrategy) with
// foreground cascade deletion.
//
// StatusWatcherStrategy is intentionally avoided for uninstall: it has a known
// race condition where cluster-scoped resources (ClusterRole, ClusterRoleBinding,
// ServiceAccount) are reported as Terminating indefinitely even after Kubernetes
// has already deleted them, causing the call to block until timeout.
// See https://github.com/helm/helm/issues/31766.
func (c *Client) DeleteRelease(ctx context.Context, project, environment string, wait bool) error {
	if project == "" || environment == "" {
		return errors.New("project or environment cannot be empty")
	}

	releaseName := GenerateReleaseName(project, environment)
	un := action.NewUninstall(c.config)
	un.IgnoreNotFound = true
	un.KeepHistory = false
	un.Timeout = c.timeout

	if wait {
		un.WaitStrategy = kube.LegacyStrategy
		un.DeletionPropagation = "foreground"
	} else {
		un.WaitStrategy = kube.HookOnlyStrategy
	}

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
	return releaserListToV1(releases)
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

// releaserToV1 narrows a Helm v4 release.Releaser back to *v1.Release.
//
// As of helm.sh/helm/v4 v4.2.0 the action package returns release.Releaser
// (aliased to `any`) so it can later carry the v2 release type alongside
// v1. The v2 type currently lives under helm.sh/helm/v4/internal/release/v2
// and is not importable from outside Helm, so every Releaser handed to us
// today is in practice a *v1.Release.
//
// Mirrors helm.sh/helm/v4/pkg/cmd/root.go:releaserToV1Release, which is the
// pattern the upstream `helm` CLI uses for the same downcast.
//
// TODO(deployah): revisit once helm.sh/helm/v4/pkg/release/v2 is exported,
// at which point we should expose a richer interface to our callers instead
// of forcing v1.
func releaserToV1(r release.Releaser) (*v1.Release, error) {
	switch rel := r.(type) {
	case *v1.Release:
		return rel, nil
	case v1.Release:
		return &rel, nil
	default:
		return nil, fmt.Errorf("unsupported helm release type %T", r)
	}
}

// releaserListToV1 narrows a slice of release.Releaser to []*v1.Release,
// preserving order and nil entries to match upstream Helm semantics (see
// helm.sh/helm/v4/pkg/cmd/root.go:releaseListToV1List).
func releaserListToV1(rs []release.Releaser) ([]*v1.Release, error) {
	out := make([]*v1.Release, 0, len(rs))
	for _, r := range rs {
		if r == nil {
			out = append(out, nil)
			continue
		}
		rel, err := releaserToV1(r)
		if err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, nil
}

// wrapHelmError provides better error messages for common Helm errors.
func (c *Client) wrapHelmError(operation, releaseName string, err error) error {
	if errors.Is(err, driver.ErrReleaseNotFound) {
		return fmt.Errorf("release '%s': %w", releaseName, ErrReleaseNotFound)
	}
	errMsg := err.Error()

	// Check for common error patterns and provide helpful messages.
	// Helm and Kubernetes internals do not expose typed sentinels for most of
	// these cases, so string matching is the only option; we always wrap the
	// original error with %w so callers can still unwrap the chain.
	switch {
	case strings.Contains(errMsg, "not found"):
		return fmt.Errorf("release '%s': %w", releaseName, ErrReleaseNotFound)
	case strings.Contains(errMsg, "another operation") || strings.Contains(errMsg, "pending"):
		return fmt.Errorf("release '%s': %w", releaseName, ErrReleasePending)
	case strings.Contains(errMsg, "timeout"):
		return fmt.Errorf("operation timed out for release '%s': %w", releaseName, err)
	case strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "dial"):
		return fmt.Errorf("unable to connect to Kubernetes cluster: %w", err)
	case strings.Contains(errMsg, "forbidden") || strings.Contains(errMsg, "unauthorized"):
		return fmt.Errorf("insufficient permissions for %s operation on release '%s': %w", operation, releaseName, err)
	case strings.Contains(errMsg, "already exists"):
		return fmt.Errorf("release '%s': %w", releaseName, ErrReleaseAlreadyExists)
	default:
		return fmt.Errorf("helm %s failed for release '%s': %w", operation, releaseName, err)
	}
}

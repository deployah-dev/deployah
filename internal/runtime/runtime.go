package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/deployah-dev/deployah/internal/helm"
	"github.com/deployah-dev/deployah/internal/manifest"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// runtimeKey is a private context key for storing the Runtime in context
type runtimeKey struct{}

// Runtime holds per-invocation state and lazily initialized clients/resources.
// It implements the RuntimeProvider interface for dependency injection.
type Runtime struct {
	namespace    string
	kubeconfig   string
	manifestPath string

	// Options for client configuration
	storageDriver string
	debug         bool
	timeout       time.Duration

	logger   LoggerProvider
	helm     HelmClient
	k8s      KubernetesClient
	manifest *manifest.Manifest
	mu       sync.Mutex

	// Factory functions for creating clients (enables testing)
	helmFactory func(*Runtime) (HelmClient, error)
	k8sFactory  func(*Runtime) (KubernetesClient, error)
}

// Option defines a functional option for configuring Runtime.
type Option func(*Runtime)

// WithNamespace sets the Kubernetes namespace.
func WithNamespace(namespace string) Option {
	return func(r *Runtime) {
		r.namespace = namespace
	}
}

// WithKubeconfig sets the kubeconfig file path.
func WithKubeconfig(kubeconfig string) Option {
	return func(r *Runtime) {
		r.kubeconfig = kubeconfig
	}
}

// WithManifestPath sets the manifest file path.
func WithManifestPath(manifestPath string) Option {
	return func(r *Runtime) {
		r.manifestPath = manifestPath
	}
}

// WithStorageDriver sets the storage driver (default: "secret").
func WithStorageDriver(driver string) Option {
	return func(r *Runtime) {
		r.storageDriver = driver
	}
}

// WithDebug controls whether to keep temporary chart directories.
func WithDebug(keep bool) Option {
	return func(r *Runtime) {
		r.debug = keep
	}
}

// WithTimeout sets the timeout for Helm operations.
func WithTimeout(timeout time.Duration) Option {
	return func(r *Runtime) {
		r.timeout = timeout
	}
}

// WithLogger sets a custom logger.
func WithLogger(logger LoggerProvider) Option {
	return func(r *Runtime) {
		r.logger = logger
	}
}

// WithHelmFactory sets a custom Helm client factory for testing.
func WithHelmFactory(factory func(*Runtime) (HelmClient, error)) Option {
	return func(r *Runtime) {
		r.helmFactory = factory
	}
}

// WithKubernetesFactory sets a custom Kubernetes client factory for testing.
func WithKubernetesFactory(factory func(*Runtime) (KubernetesClient, error)) Option {
	return func(r *Runtime) {
		r.k8sFactory = factory
	}
}

// defaultHelmFactory creates a default Helm client.
func defaultHelmFactory(r *Runtime) (HelmClient, error) {
	var opts []helm.Option
	if r.namespace != "" {
		opts = append(opts, helm.WithNamespace(r.namespace))
	}
	if r.kubeconfig != "" {
		opts = append(opts, helm.WithKubeconfig(r.kubeconfig))
	}
	if r.storageDriver != "" {
		opts = append(opts, helm.WithStorageDriver(r.storageDriver))
	}
	if r.timeout > 0 {
		opts = append(opts, helm.WithTimeout(r.timeout))
	}
	if r.debug {
		opts = append(opts, helm.WithDebug(r.debug))
	}

	return helm.NewClient(opts...)
}

// defaultKubernetesFactory creates a default Kubernetes client.
func defaultKubernetesFactory(r *Runtime) (KubernetesClient, error) {
	// Try in-cluster config first
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig resolution
		if r.kubeconfig != "" {
			// Use explicit kubeconfig path
			cfg, err = clientcmd.BuildConfigFromFlags("", r.kubeconfig)
		} else {
			// Use default loading rules (KUBECONFIG or ~/.kube/config)
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			overrides := &clientcmd.ConfigOverrides{}
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return cs, nil
}

// New constructs a Runtime with functional options.
func New(options ...Option) *Runtime {
	r := &Runtime{
		storageDriver: DefaultStorageDriver,
		timeout:       DefaultTimeout,
		helmFactory:   defaultHelmFactory,
		k8sFactory:    defaultKubernetesFactory,
	}

	for _, option := range options {
		option(r)
	}

	return r
}

// WithRuntime returns a new context carrying the provided runtime.
func WithRuntime(ctx context.Context, rt *Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// FromRuntime extracts a Runtime from the command context, or nil if absent.
func FromRuntime(ctx context.Context) *Runtime {
	if v := ctx.Value(runtimeKey{}); v != nil {
		if rt, ok := v.(*Runtime); ok {
			return rt
		}
	}
	return nil
}

// Helm returns a memoized Helm client configured for this runtime.
func (r *Runtime) Helm() (HelmClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.helm != nil {
		return r.helm, nil
	}

	c, err := r.helmFactory(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create helm client (namespace=%q, kubeconfig=%q): %w",
			r.namespace, r.kubeconfig, err)
	}
	r.helm = c
	return r.helm, nil
}

// Kubernetes returns a memoized Kubernetes clientset configured for this runtime.
func (r *Runtime) Kubernetes() (KubernetesClient, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.k8s != nil {
		return r.k8s, nil
	}

	cs, err := r.k8sFactory(r)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}
	r.k8s = cs
	return r.k8s, nil
}

// Manifest loads and memoizes the manifest for the configured path and environment.
func (r *Runtime) Manifest(ctx context.Context, environment string) (*manifest.Manifest, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.manifest != nil {
		return r.manifest, nil
	}
	if r.manifestPath == "" {
		return nil, fmt.Errorf("manifest path must be set")
	}
	m, err := manifest.Load(ctx, r.manifestPath, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to load manifest: %w", err)
	}
	r.manifest = m
	return m, nil
}

// Close performs cleanup of resources held by the runtime.
// It's safe to call multiple times.
func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Currently, the k8s client and helm client don't require explicit cleanup,
	// but this method provides a hook for future cleanup needs
	r.helm = nil
	r.k8s = nil
	r.manifest = nil

	return nil
}

// DebugKeepTempChart returns whether temporary chart directories should be kept.
func (r *Runtime) DebugKeepTempChart() bool { return r.debug }

// Timeout returns the configured timeout for Helm operations.
func (r *Runtime) Timeout() time.Duration { return r.timeout }

// Namespace returns the configured namespace, or "default" if none is set.
func (r *Runtime) Namespace() string {
	if r.namespace != "" {
		return r.namespace
	}
	return DefaultNamespace
}

// RESTConfig returns a Kubernetes REST config using the same logic as the runtime's K8s client.
func (r *Runtime) RESTConfig() (*rest.Config, error) {
	// Try in-cluster config first
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig resolution
		if r.kubeconfig != "" {
			// Use explicit kubeconfig path
			cfg, err = clientcmd.BuildConfigFromFlags("", r.kubeconfig)
		} else {
			// Use default loading rules (KUBECONFIG or ~/.kube/config)
			loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
			overrides := &clientcmd.ConfigOverrides{}
			cfg, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
		}
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
		}
	}
	return cfg, nil
}

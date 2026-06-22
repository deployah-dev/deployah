package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"
)

// runtimeKey is a private context key for storing the Runtime in context
type runtimeKey struct{}

// Runtime holds per-invocation state and lazily initialized clients/resources.
// It implements the RuntimeProvider interface for dependency injection.
type Runtime struct {
	namespace            string
	kubeconfig           string
	kubeContext          string
	extraKubeconfigPaths []string
	specPath             string

	// Options for client configuration
	storageDriver string
	debug         bool
	timeout       time.Duration

	logger LoggerProvider
	helm   HelmClient
	k8s    KubernetesClient
	spec   *spec.Spec
	mu     sync.Mutex

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

// WithKubeContext sets the Kubernetes context to use, overriding the
// kubeconfig's current context. An empty value leaves the current context in
// effect.
func WithKubeContext(kubeContext string) Option {
	return func(r *Runtime) {
		r.kubeContext = kubeContext
	}
}

// WithExtraKubeconfigPaths appends additional kubeconfig file paths to the
// clientcmd loading-rules Precedence list. This makes contexts from those files
// available without polluting the user's default kubeconfig. Missing files are
// silently skipped by client-go. An explicit --kubeconfig flag still takes
// priority because it sets ExplicitPath, which causes Precedence to be ignored.
func WithExtraKubeconfigPaths(paths ...string) Option {
	return func(r *Runtime) {
		r.extraKubeconfigPaths = append(r.extraKubeconfigPaths, paths...)
	}
}

// WithSpecPath sets the spec file path.
func WithSpecPath(specPath string) Option {
	return func(r *Runtime) {
		r.specPath = specPath
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
	if r.kubeContext != "" {
		opts = append(opts, helm.WithKubeContext(r.kubeContext))
	}
	if len(r.extraKubeconfigPaths) > 0 {
		opts = append(opts, helm.WithExtraKubeconfigPaths(r.extraKubeconfigPaths...))
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
		// Fall back to kubeconfig resolution (honoring an explicit path and/or
		// a context override).
		cfg, err = r.kubeconfigRESTConfig()
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

// WithContext returns a new context carrying the provided runtime.
func WithContext(ctx context.Context, rt *Runtime) context.Context {
	return context.WithValue(ctx, runtimeKey{}, rt)
}

// FromContext extracts a Runtime from the command context, or nil if absent.
func FromContext(ctx context.Context) *Runtime {
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

// Spec loads and memoizes the spec for the configured path and
// environment.
func (r *Runtime) Spec(ctx context.Context, environment string) (*spec.Spec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.spec != nil {
		return r.spec, nil
	}
	if r.specPath == "" {
		return nil, fmt.Errorf("spec path must be set")
	}
	m, err := spec.Load(ctx, r.specPath, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}
	r.spec = m
	return m, nil
}

// SetKubeContext sets the Kubernetes context to target and invalidates any
// memoized clients so they are rebuilt with the new context on next use.
//
// It is intended to be called after the spec is loaded (so an
// environment's "context" field can be applied) but before the Helm or
// Kubernetes clients are first built. A non-empty context set here takes
// effect; callers enforce precedence (flag over environment field) by only
// passing the resolved value.
func (r *Runtime) SetKubeContext(kubeContext string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.kubeContext == kubeContext {
		return
	}
	r.kubeContext = kubeContext
	r.helm = nil
	r.k8s = nil
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
	r.spec = nil

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

// RESTConfig returns a Kubernetes REST config using the same logic as the
// runtime Kubernetes client.
func (r *Runtime) RESTConfig() (*rest.Config, error) {
	// Try in-cluster config first
	cfg, err := rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig resolution (honoring an explicit path and/or
		// a context override).
		cfg, err = r.kubeconfigRESTConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
		}
	}
	return cfg, nil
}

// kubeconfigRESTConfig builds a REST config from kubeconfig resolution rules,
// honoring an explicit kubeconfig path (r.kubeconfig) and/or a context
// override (r.kubeContext). When neither is set it behaves like the default
// KUBECONFIG/~/.kube/config resolution with the current context.
func (r *Runtime) kubeconfigRESTConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if r.kubeconfig != "" {
		loadingRules.ExplicitPath = r.kubeconfig
	} else if len(r.extraKubeconfigPaths) > 0 {
		// Prepend deployah-managed kubeconfig files so they take priority
		// over ~/.kube/config when both define the same context name (e.g.
		// a recreated Kind cluster on a new port). ExplicitPath takes full
		// precedence when --kubeconfig is given, so we only extend
		// Precedence when no explicit path was provided.
		loadingRules.Precedence = append(r.extraKubeconfigPaths, loadingRules.Precedence...)
	}
	overrides := &clientcmd.ConfigOverrides{}
	if r.kubeContext != "" {
		overrides.CurrentContext = r.kubeContext
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

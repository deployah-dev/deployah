package session

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

// sessionKey is a private context key for storing the Session in context.
type sessionKey struct{}

// Session holds per-invocation configuration and lazily loads the spec.
// It is created once in the root pre-run hook and travels through
// [context.Context] so every command shares one configured environment.
//
// To access Helm or Kubernetes clients, call [Session.Target] first.
type Session struct {
	namespace            string
	kubeconfig           string
	kubeContext          string
	extraKubeconfigPaths []string
	specPath             string

	storageDriver string
	debug         bool
	timeout       time.Duration

	spec *spec.Spec
	mu   sync.Mutex

	helmFactory func(*Session) (HelmClient, error)
	k8sFactory  func(*Session) (kubernetes.Interface, error)
}

// Option is a functional option for configuring a [Session].
type Option func(*Session)

// WithNamespace sets the Kubernetes namespace.
func WithNamespace(namespace string) Option {
	return func(s *Session) { s.namespace = namespace }
}

// WithKubeconfig sets the kubeconfig file path.
func WithKubeconfig(kubeconfig string) Option {
	return func(s *Session) { s.kubeconfig = kubeconfig }
}

// WithKubeContext sets the Kubernetes context, overriding the kubeconfig's
// current context. An empty value leaves the current context in effect.
func WithKubeContext(kubeContext string) Option {
	return func(s *Session) { s.kubeContext = kubeContext }
}

// WithExtraKubeconfigPaths appends additional kubeconfig file paths to the
// clientcmd loading-rules Precedence list, making contexts from those files
// available without polluting the user's default kubeconfig. Missing files are
// silently skipped by client-go. An explicit --kubeconfig flag still takes
// priority because it sets ExplicitPath, which causes Precedence to be ignored.
func WithExtraKubeconfigPaths(paths ...string) Option {
	return func(s *Session) {
		s.extraKubeconfigPaths = append(s.extraKubeconfigPaths, paths...)
	}
}

// WithSpecPath sets the spec file path.
func WithSpecPath(specPath string) Option {
	return func(s *Session) { s.specPath = specPath }
}

// WithStorageDriver sets the Helm storage driver (default: "secret").
func WithStorageDriver(driver string) Option {
	return func(s *Session) { s.storageDriver = driver }
}

// WithDebug controls whether to keep temporary chart directories.
func WithDebug(keep bool) Option {
	return func(s *Session) { s.debug = keep }
}

// WithTimeout sets the timeout for Helm operations.
func WithTimeout(timeout time.Duration) Option {
	return func(s *Session) { s.timeout = timeout }
}

// WithHelmFactory sets a custom Helm client factory, primarily for testing.
func WithHelmFactory(factory func(*Session) (HelmClient, error)) Option {
	return func(s *Session) { s.helmFactory = factory }
}

// WithKubernetesFactory sets a custom Kubernetes client factory,
// primarily for testing.
func WithKubernetesFactory(factory func(*Session) (kubernetes.Interface, error)) Option {
	return func(s *Session) { s.k8sFactory = factory }
}

// New constructs a Session with the given functional options.
func New(options ...Option) *Session {
	s := &Session{
		storageDriver: DefaultStorageDriver,
		timeout:       DefaultTimeout,
		helmFactory:   defaultHelmFactory,
		k8sFactory:    defaultKubernetesFactory,
	}
	for _, opt := range options {
		opt(s)
	}
	return s
}

// WithContext returns a new context carrying sess.
func WithContext(ctx context.Context, sess *Session) context.Context {
	return context.WithValue(ctx, sessionKey{}, sess)
}

// FromContext extracts the Session from ctx, or nil if absent.
func FromContext(ctx context.Context) *Session {
	if v := ctx.Value(sessionKey{}); v != nil {
		if s, ok := v.(*Session); ok {
			return s
		}
	}
	return nil
}

// Spec loads and memoizes the spec for the configured path and environment.
func (s *Session) Spec(ctx context.Context, environment string) (*spec.Spec, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.spec != nil {
		return s.spec, nil
	}
	if s.specPath == "" {
		return nil, fmt.Errorf("spec path must be set")
	}
	m, err := spec.Load(ctx, s.specPath, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}
	s.spec = m
	return m, nil
}

// Target resolves the Kubernetes context for env and returns a [Cluster] from
// which Helm and Kubernetes clients can be obtained.
//
// Precedence for the kubeContext used by the returned Cluster:
//  1. The global --context flag (already stored in s.kubeContext).
//  2. The environment's "context" field in the spec (loaded when env is non-empty
//     and the global flag is absent).
//  3. The default context from the active kubeconfig (empty string).
//
// If the spec file cannot be loaded when env is non-empty, Target falls back
// silently to the default context rather than returning an error. Commands that
// require the spec (e.g. deploy) load it explicitly afterwards and will surface
// a proper error to the user.
func (s *Session) Target(ctx context.Context, env string) (*Cluster, error) {
	kubeCtx := s.kubeContext
	var resolved *spec.Spec

	if kubeCtx == "" && env != "" {
		if m, err := s.Spec(ctx, env); err == nil {
			resolved = m
			if envCtx := environmentContext(m, env); envCtx != "" {
				kubeCtx = envCtx
			}
		}
		// silently fall back to default context if spec loading fails
	}

	return &Cluster{
		Session:      s,
		resolvedSpec: resolved,
		kubeContext:  kubeCtx,
	}, nil
}

// DebugKeepTempChart reports whether temporary chart directories should be kept.
func (s *Session) DebugKeepTempChart() bool { return s.debug }

// Timeout returns the configured timeout for Helm operations.
func (s *Session) Timeout() time.Duration { return s.timeout }

// Close releases memoized resources held by the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.spec = nil
	return nil
}

// cloneWithContext returns a shallow copy of s with the given kubeContext
// applied, clearing any cached clients. Used by Cluster to build clients
// with the resolved context without mutating the original session.
func (s *Session) cloneWithContext(kubeContext string) *Session {
	return &Session{
		namespace:            s.namespace,
		kubeconfig:           s.kubeconfig,
		kubeContext:          kubeContext,
		extraKubeconfigPaths: s.extraKubeconfigPaths,
		specPath:             s.specPath,
		storageDriver:        s.storageDriver,
		debug:                s.debug,
		timeout:              s.timeout,
		helmFactory:          s.helmFactory,
		k8sFactory:           s.k8sFactory,
	}
}

// defaultHelmFactory creates a Helm client from session configuration.
func defaultHelmFactory(s *Session) (HelmClient, error) {
	var opts []helm.Option
	if s.namespace != "" {
		opts = append(opts, helm.WithNamespace(s.namespace))
	}
	if s.kubeconfig != "" {
		opts = append(opts, helm.WithKubeconfig(s.kubeconfig))
	}
	if s.kubeContext != "" {
		opts = append(opts, helm.WithKubeContext(s.kubeContext))
	}
	if len(s.extraKubeconfigPaths) > 0 {
		opts = append(opts, helm.WithExtraKubeconfigPaths(s.extraKubeconfigPaths...))
	}
	if s.storageDriver != "" {
		opts = append(opts, helm.WithStorageDriver(s.storageDriver))
	}
	if s.timeout > 0 {
		opts = append(opts, helm.WithTimeout(s.timeout))
	}
	if s.debug {
		opts = append(opts, helm.WithDebug(s.debug))
	}
	return helm.NewClient(opts...)
}

// defaultKubernetesFactory creates a Kubernetes clientset from session
// configuration, preferring in-cluster config when available.
func defaultKubernetesFactory(s *Session) (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = s.kubeconfigRESTConfig()
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

// kubeconfigRESTConfig builds a REST config from kubeconfig resolution rules,
// honoring an explicit kubeconfig path (s.kubeconfig) and/or a context
// override (s.kubeContext). When neither is set it uses the default
// KUBECONFIG/~/.kube/config resolution with the current context.
func (s *Session) kubeconfigRESTConfig() (*rest.Config, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	} else if len(s.extraKubeconfigPaths) > 0 {
		// Prepend deployah-managed kubeconfig files so they take priority
		// over ~/.kube/config when both define the same context name (e.g.
		// a recreated Kind cluster on a new port). ExplicitPath takes full
		// precedence when --kubeconfig is given, so we only extend
		// Precedence when no explicit path was provided.
		loadingRules.Precedence = append(s.extraKubeconfigPaths, loadingRules.Precedence...)
	}
	overrides := &clientcmd.ConfigOverrides{}
	if s.kubeContext != "" {
		overrides.CurrentContext = s.kubeContext
	}
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
}

// environmentContext returns the "context" field of the named environment in
// the spec, or an empty string when the environment is not found or has no
// context set.
func environmentContext(m *spec.Spec, name string) string {
	if m == nil {
		return ""
	}
	for _, env := range m.Environments {
		if env.Name == name {
			return env.Context
		}
	}
	return ""
}

// Cluster is a resolved target: it embeds the base [Session] and adds a
// confirmed Kubernetes context plus lazily-initialized Helm and Kubernetes
// clients. Obtain one via [Session.Target].
type Cluster struct {
	*Session
	resolvedSpec *spec.Spec
	kubeContext  string

	helm HelmClient
	k8s  kubernetes.Interface
	mu   sync.Mutex
}

// Spec returns the spec that was loaded during [Session.Target], or nil if
// Target was called without an environment or the spec could not be loaded.
func (cl *Cluster) Spec() *spec.Spec { return cl.resolvedSpec }

// Namespace returns the configured namespace, or "default" if none is set.
func (cl *Cluster) Namespace() string {
	if cl.namespace != "" {
		return cl.namespace
	}
	return DefaultNamespace
}

// Helm returns a memoized Helm client targeted at the resolved cluster.
func (cl *Cluster) Helm() (HelmClient, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.helm != nil {
		return cl.helm, nil
	}
	tmp := cl.cloneWithContext(cl.kubeContext)
	c, err := tmp.helmFactory(tmp)
	if err != nil {
		return nil, fmt.Errorf("helm client (namespace=%q, kubeconfig=%q): %w",
			cl.namespace, cl.kubeconfig, err)
	}
	cl.helm = c
	return cl.helm, nil
}

// Kubernetes returns a memoized Kubernetes clientset targeted at the
// resolved cluster.
func (cl *Cluster) Kubernetes() (kubernetes.Interface, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.k8s != nil {
		return cl.k8s, nil
	}
	tmp := cl.cloneWithContext(cl.kubeContext)
	cs, err := tmp.k8sFactory(tmp)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	cl.k8s = cs
	return cl.k8s, nil
}

// RESTConfig returns a Kubernetes REST config for the resolved cluster.
func (cl *Cluster) RESTConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		tmp := cl.cloneWithContext(cl.kubeContext)
		cfg, err = tmp.kubeconfigRESTConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to build kubernetes config: %w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
		}
	}
	return cfg, nil
}

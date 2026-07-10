package session

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

// CommandPolicy controls how the session handles missing platform files.
type CommandPolicy int

const (
	// PolicyLenient allows proceeding with a warning when the platform file
	// is absent. Used for read-only commands (logs, status, list, shell).
	PolicyLenient CommandPolicy = iota
	// PolicyStrict requires a resolvable platform file for commands that
	// modify cluster state (deploy, delete). The caller must gate on this
	// before contacting the cluster.
	PolicyStrict
)

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
	platformPath         string
	commandPolicy        CommandPolicy

	storageDriver string
	debug         bool
	timeout       time.Duration

	platform *spec.PlatformConfig
	mu       sync.Mutex

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

// WithPlatformFile sets an explicit platform file path, overriding both the
// DEPLOYAH_PLATFORM_FILE environment variable and the same-directory default.
func WithPlatformFile(path string) Option {
	return func(s *Session) { s.platformPath = path }
}

// WithCommandPolicy sets the platform-missing policy for this session.
// Destructive commands (deploy, delete) should use [PolicyStrict];
// read-only commands (logs, status, list, shell) should use [PolicyLenient].
func WithCommandPolicy(policy CommandPolicy) Option {
	return func(s *Session) { s.commandPolicy = policy }
}

// CommandPolicy returns the configured platform-missing policy.
func (s *Session) CommandPolicy() CommandPolicy { return s.commandPolicy }

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

// Platform loads and memoizes the platform configuration. It resolves the
// platform file path from (in order of precedence):
//  1. An explicit path set via [WithPlatformFile].
//  2. The DEPLOYAH_PLATFORM_FILE environment variable.
//  3. The same directory as the spec file (deployah.platform.yaml).
//
// When no platform file is found, Platform returns (nil, nil). Only when a
// path is found but fails to load does it return an error.
func (s *Session) Platform() (*spec.PlatformConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.platform != nil {
		return s.platform, nil
	}
	explicit := s.platformPath != "" || os.Getenv(spec.PlatformEnvVar) != ""
	path := s.resolvePlatformPath()
	if !explicit {
		// The default-path lookup treats an absent file as "no platform
		// config"; only an explicitly named file must exist.
		if _, err := os.Stat(path); err != nil {
			return nil, nil //nolint:nilnil // absent platform file is not an error; callers check for nil config
		}
	}
	p, err := spec.LoadPlatform(path)
	if err != nil {
		return nil, err
	}
	s.platform = p
	return p, nil
}

// resolvePlatformPath returns the platform file path following the lookup
// precedence rule. It is called with s.mu held.
func (s *Session) resolvePlatformPath() string {
	if s.platformPath != "" {
		return s.platformPath
	}
	if envPath := os.Getenv(spec.PlatformEnvVar); envPath != "" {
		return envPath
	}
	// Same directory as the spec file.
	if s.specPath != "" {
		dir := filepath.Dir(s.specPath)
		return filepath.Join(dir, spec.DefaultPlatformPath)
	}
	return spec.DefaultPlatformPath
}

// Spec loads the spec for the configured path and environment. Each call loads
// from disk because the result depends on the environment argument (envsubst
// selects different env files per environment). The platform config, when
// present, supplies the environment registry.
func (s *Session) Spec(ctx context.Context, environment string) (*spec.Spec, error) {
	if s.specPath == "" {
		return nil, fmt.Errorf("spec path must be set")
	}
	platform, err := s.Platform()
	if err != nil {
		return nil, fmt.Errorf("failed to load platform file: %w", err)
	}
	m, err := spec.Load(ctx, s.specPath, environment, platform)
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}
	return m, nil
}

// Target resolves the Kubernetes context for env and returns a [Cluster] from
// which Helm and Kubernetes clients can be obtained.
//
// Precedence for the kubeContext used by the returned Cluster:
//  1. The global --context flag (already stored in s.kubeContext).
//  2. The platform file's context for env (via [PlatformEnvContext]).
//  3. The default context from the active kubeconfig (empty string).
func (s *Session) Target(ctx context.Context, env string) (*Cluster, error) {
	kubeCtx := s.kubeContext

	// When no --context override, try the platform file.
	if kubeCtx == "" && env != "" {
		if p, err := s.Platform(); err == nil && p != nil {
			if pCtx := spec.PlatformEnvContext(p, env); pCtx != "" {
				kubeCtx = pCtx
			}
		}
	}

	cluster := &Cluster{
		Session:     s,
		kubeContext: kubeCtx,
	}
	if kubeCtx == "" {
		// Record the fallback where it happens so every command can warn
		// about it consistently.
		cluster.fallbackContext = s.CurrentKubeContext()
		cluster.usedFallback = true
	}
	return cluster, nil
}

// SpecPath returns the configured spec file path.
func (s *Session) SpecPath() string { return s.specPath }

// ParseManifest reads and partially validates the spec (apiVersion +
// environments only, no envsubst, no defaults). It is intended for commands
// that need the raw manifest structure without environment-specific processing
// (e.g. validate manifest-only mode, resolve offline mode).
func (s *Session) ParseManifest() (*spec.Spec, error) {
	rawSpec, _, err := spec.ParseManifest(s.specPath)
	if err != nil {
		return nil, err
	}
	return rawSpec, nil
}

// KubeContext returns the explicit kube context override, or empty string if
// none was set. An empty string means the cluster context comes from the
// platform file or kubeconfig default.
func (s *Session) KubeContext() string { return s.kubeContext }

// DebugKeepTempChart reports whether temporary chart directories should be kept.
func (s *Session) DebugKeepTempChart() bool { return s.debug }

// Timeout returns the configured timeout for Helm operations.
func (s *Session) Timeout() time.Duration { return s.timeout }

// Close releases memoized resources held by the session.
func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.platform = nil
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
		platformPath:         s.platformPath,
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

// CurrentKubeContext returns the current-context name from the active
// kubeconfig resolution (explicit --kubeconfig path, deployah-managed extra
// paths, then KUBECONFIG/~/.kube/config), ignoring any --context override.
// Returns an empty string when no kubeconfig is readable.
func (s *Session) CurrentKubeContext() string {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if s.kubeconfig != "" {
		loadingRules.ExplicitPath = s.kubeconfig
	} else if len(s.extraKubeconfigPaths) > 0 {
		loadingRules.Precedence = append(s.extraKubeconfigPaths, loadingRules.Precedence...)
	}
	cfg, err := loadingRules.Load()
	if err != nil {
		return ""
	}
	return cfg.CurrentContext
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

// Cluster is a resolved target: it embeds the base [Session] and adds a
// confirmed Kubernetes context plus lazily-initialized Helm and Kubernetes
// clients. Obtain one via [Session.Target].
type Cluster struct {
	*Session
	kubeContext string

	// usedFallback is true when neither --context nor a platform context
	// resolved, so clients follow the kubeconfig current-context
	// (fallbackContext, empty when no kubeconfig is readable).
	usedFallback    bool
	fallbackContext string

	helm HelmClient
	k8s  kubernetes.Interface
	mu   sync.Mutex
}

// ContextFallback reports whether the target follows the kubeconfig
// current-context, and that context's name.
func (cl *Cluster) ContextFallback() (bool, string) {
	return cl.usedFallback, cl.fallbackContext
}

// Context returns the resolved Kubernetes context for this cluster target.
// An empty string means the default context from the active kubeconfig is used.
func (cl *Cluster) Context() string { return cl.kubeContext }

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

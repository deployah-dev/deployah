package session

import (
	"context"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"
	"deployah.dev/deployah/internal/target"
	"deployah.dev/deployah/internal/workspace"
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
// Spec and platform source loading is delegated to [workspace.Workspace].
// Kubernetes destination resolution is delegated to [target.Resolver].
// To access Helm or Kubernetes clients, call [Session.Target] first.
type Session struct {
	workspaceConfig workspace.Config
	workspace       *workspace.Workspace

	namespace            string
	kubeconfig           string
	kubeContext          string
	extraKubeconfigPaths []string
	commandPolicy        CommandPolicy

	storageDriver string
	debug         bool
	timeout       time.Duration

	helmFactory func(*Session) (HelmClient, error)
	k8sFactory  func(*target.Target) (kubernetes.Interface, error)
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
	return func(s *Session) { s.workspaceConfig.SpecPath = specPath }
}

// WithPlatformFile sets an explicit platform file path, overriding both the
// DEPLOYAH_PLATFORM_FILE environment variable and the same-directory default.
func WithPlatformFile(path string) Option {
	return func(s *Session) { s.workspaceConfig.PlatformPath = path }
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
func WithKubernetesFactory(factory func(*target.Target) (kubernetes.Interface, error)) Option {
	return func(s *Session) { s.k8sFactory = factory }
}

// New constructs a Session with the given functional options.
// The composed [workspace.Workspace] is created after all options are applied.
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
	s.workspace = workspace.New(s.workspaceConfig)
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

// Platform loads and memoizes the platform configuration through the
// composed [workspace.Workspace]. See [workspace.Workspace.Platform] for
// required versus optional source semantics.
func (s *Session) Platform() (*spec.PlatformConfig, error) {
	return s.workspace.Platform()
}

// Spec loads the spec for [Session.SpecPath] and environment. Each call loads
// from disk because the result depends on the environment argument (envsubst
// selects different env files per environment). The platform config, when
// present, supplies the environment registry.
func (s *Session) Spec(ctx context.Context, environment string) (*spec.Spec, error) {
	return s.workspace.LoadSpec(ctx, environment)
}

// Target resolves the Kubernetes destination for env and returns a [Cluster]
// from which Helm and Kubernetes clients can be obtained.
//
// Destination resolution is delegated to [target.Resolver]. The platform
// file, when present, supplies only a context name via
// [spec.PlatformEnvContext]. ctx is unused.
func (s *Session) Target(ctx context.Context, env string) (*Cluster, error) {
	platformContext := ""
	if env != "" {
		if p, err := s.Platform(); err == nil && p != nil {
			platformContext = spec.PlatformEnvContext(p, env)
		}
	}
	t := target.NewResolver(s.targetConfig()).Resolve(platformContext)
	return &Cluster{Session: s, target: t}, nil
}

func (s *Session) targetConfig() target.Config {
	return target.Config{
		KubeconfigPath:       s.kubeconfig,
		ContextOverride:      s.kubeContext,
		NamespaceOverride:    s.namespace,
		ExtraKubeconfigPaths: s.extraKubeconfigPaths,
	}
}

// SpecPath returns the effective spec file path from the composed Workspace.
// The path is never empty; an unset option defaults to [spec.DefaultSpecPath].
func (s *Session) SpecPath() string {
	return s.workspace.SpecPath()
}

// PlatformPath returns the snapshotted platform file path from the
// composed Workspace.
func (s *Session) PlatformPath() string {
	return s.workspace.PlatformPath()
}

// ParseManifest reads and partially validates the spec (apiVersion +
// environments only, no envsubst, no defaults). It is intended for commands
// that need the raw manifest structure without environment-specific processing
// (e.g. validate manifest-only mode, substitution prescan).
func (s *Session) ParseManifest() (*spec.Spec, error) {
	return s.workspace.ParseManifest()
}

// KubeContext returns the explicit kube context override, or empty string if
// none was set. An empty string means the cluster context comes from the
// platform file or kubeconfig default.
func (s *Session) KubeContext() string { return s.kubeContext }

// DebugKeepTempChart reports whether temporary chart directories should be kept.
func (s *Session) DebugKeepTempChart() bool { return s.debug }

// Timeout returns the configured timeout for Helm operations.
func (s *Session) Timeout() time.Duration { return s.timeout }

// cloneWithContext returns a shallow copy of s with the given kubeContext
// applied, clearing any cached clients. Used by Cluster to build clients
// with the resolved context without mutating the original session.
// The clone shares the same [workspace.Workspace].
func (s *Session) cloneWithContext(kubeContext string) *Session {
	return &Session{
		workspace:            s.workspace,
		namespace:            s.namespace,
		kubeconfig:           s.kubeconfig,
		kubeContext:          kubeContext,
		extraKubeconfigPaths: s.extraKubeconfigPaths,
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

// defaultKubernetesFactory creates a Kubernetes clientset from the resolved
// target, preferring in-cluster config when available. That in-cluster-first
// preference is transitional; destination metadata still comes from Target.
func defaultKubernetesFactory(t *target.Target) (kubernetes.Interface, error) {
	cfg, err := rest.InClusterConfig()
	if err != nil {
		cfg, err = t.RESTConfig()
		if err != nil {
			return nil, fmt.Errorf("%w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
		}
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return cs, nil
}

// CurrentKubeContext returns the current-context name from kubeconfig
// resolution (explicit --kubeconfig path, deployah-managed extra paths,
// then KUBECONFIG/~/.kube/config), ignoring any --context override.
// Returns an empty string when no kubeconfig is readable.
func (s *Session) CurrentKubeContext() string {
	return target.NewResolver(target.Config{
		KubeconfigPath:       s.kubeconfig,
		ExtraKubeconfigPaths: s.extraKubeconfigPaths,
	}).Resolve("").Context()
}

// Cluster is a resolved destination plus lazily-initialized Helm and
// Kubernetes clients. Obtain one via [Session.Target].
//
// Destination metadata is owned by [target.Target]. RESTConfig and the
// default Kubernetes factory still prefer in-cluster config when present;
// that mismatch with Target metadata is transitional.
type Cluster struct {
	*Session
	target *target.Target

	helm HelmClient
	k8s  kubernetes.Interface
	mu   sync.Mutex
}

// Context returns the effective Kubernetes context for this destination.
func (cl *Cluster) Context() string { return cl.target.Context() }

// ContextSource returns which rule selected [Cluster.Context].
func (cl *Cluster) ContextSource() target.ContextSource {
	return cl.target.ContextSource()
}

// Namespace returns the effective namespace for this destination.
func (cl *Cluster) Namespace() string { return cl.target.Namespace() }

// sessionForContext returns a shallow Session copy targeted at this
// cluster's resolved destination, with cached clients cleared. Used by
// Helm so the factory still receives Session-owned settings.
func (cl *Cluster) sessionForContext() *Session {
	tmp := cl.cloneWithContext(cl.target.Context())
	tmp.namespace = cl.target.Namespace()
	return tmp
}

// Helm returns a memoized Helm client targeted at the resolved cluster.
func (cl *Cluster) Helm() (HelmClient, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.helm != nil {
		return cl.helm, nil
	}
	tmp := cl.sessionForContext()
	c, err := tmp.helmFactory(tmp)
	if err != nil {
		return nil, fmt.Errorf("helm client (namespace=%q, kubeconfig=%q): %w",
			cl.target.Namespace(), cl.kubeconfig, err)
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
	cs, err := cl.k8sFactory(cl.target)
	if err != nil {
		return nil, fmt.Errorf("kubernetes client: %w", err)
	}
	cl.k8s = cs
	return cl.k8s, nil
}

// RESTConfig returns a Kubernetes REST config for the resolved cluster.
//
// In-cluster config is tried first, matching historical Deployah behavior.
// When that is unavailable, the kubeconfig destination from [target.Target]
// is used.
func (cl *Cluster) RESTConfig() (*rest.Config, error) {
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return cfg, nil
	}
	cfg, err = cl.target.RESTConfig()
	if err != nil {
		return nil, fmt.Errorf("%w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
	}
	return cfg, nil
}

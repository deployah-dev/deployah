package session

import (
	"context"
	"fmt"
	"slices"
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

// Session holds per-invocation configuration for one CLI run.
// It is created once in the root pre-run hook and travels through
// [context.Context] so every command shares one configured environment.
//
// Spec and platform source loading is owned by [workspace.Workspace].
// Call [Session.Workspace] to load sources. Kubernetes destination
// resolution is delegated to [target.Resolver]. To access Helm or
// Kubernetes clients, call [Session.Target] first.
type Session struct {
	workspaceConfig workspace.Config
	workspace       *workspace.Workspace

	namespace            string
	kubeconfig           string
	kubeContext          string
	extraKubeconfigPaths []string

	storageDriver string
	debug         bool
	timeout       time.Duration

	helmFactory HelmFactory
	k8sFactory  func(*target.Target) (kubernetes.Interface, error)
}

// HelmConfig holds Helm client-construction inputs that are not destination
// metadata. Context and namespace come from [target.Target].
//
// [Session.Target] snapshots these values onto [Cluster]. Session accessors
// such as [Session.Timeout] keep reading the Session fields.
type HelmConfig struct {
	// KubeconfigPath is the explicit kubeconfig path snapshotted from
	// Session. Destination selection uses [target.Target], not this field.
	KubeconfigPath string
	// ExtraKubeconfigPaths is the extra kubeconfig path list snapshotted
	// from Session. Destination selection uses [target.Target], not this
	// field.
	ExtraKubeconfigPaths []string
	// StorageDriver is the Helm storage driver (secret, configmap, or memory).
	// Empty lets [helm.NewClient] use its default ("secret").
	StorageDriver string
	// Debug reports whether temporary chart directories should be kept.
	Debug bool
	// Timeout is the Helm operation timeout. Zero lets [helm.NewClient] use
	// its default (5 minutes). [Session.New] sets [DefaultTimeout] (10
	// minutes) unless [WithTimeout] overrides it.
	Timeout time.Duration
}

// HelmFactory constructs a Helm client for a resolved destination.
// It must not depend on [Session].
type HelmFactory func(*target.Target, HelmConfig) (HelmClient, error)

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
func WithHelmFactory(factory HelmFactory) Option {
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

// Workspace returns the invocation [workspace.Workspace] that owns spec
// and platform source locations. The Workspace is created in [New] and
// does not change for the rest of the Session.
func (s *Session) Workspace() *workspace.Workspace {
	return s.workspace
}

// Target resolves the Kubernetes destination for env and returns a [Cluster]
// from which Helm and Kubernetes clients can be obtained.
//
// Destination resolution is delegated to [target.Resolver]. The platform
// file, when present, supplies only a context name via
// [spec.PlatformEnvContext]. Platform load errors are ignored so Target
// can still resolve from kubeconfig. ctx is unused.
func (s *Session) Target(ctx context.Context, env string) (*Cluster, error) {
	platformContext := ""
	if env != "" {
		if p, err := s.workspace.Platform(); err == nil && p != nil {
			platformContext = spec.PlatformEnvContext(p, env)
		}
	}
	t := target.NewResolver(s.targetConfig()).Resolve(platformContext)
	return &Cluster{
		target:      t,
		helmConfig:  s.snapshotHelmConfig(),
		helmFactory: s.helmFactory,
		k8sFactory:  s.k8sFactory,
	}, nil
}

func (s *Session) snapshotHelmConfig() HelmConfig {
	return HelmConfig{
		KubeconfigPath:       s.kubeconfig,
		ExtraKubeconfigPaths: slices.Clone(s.extraKubeconfigPaths),
		StorageDriver:        s.storageDriver,
		Debug:                s.debug,
		Timeout:              s.timeout,
	}
}

func (s *Session) targetConfig() target.Config {
	return target.Config{
		KubeconfigPath:       s.kubeconfig,
		ContextOverride:      s.kubeContext,
		NamespaceOverride:    s.namespace,
		ExtraKubeconfigPaths: s.extraKubeconfigPaths,
	}
}

// KubeContext returns the explicit kube context override, or empty string if
// none was set. An empty string means the cluster context comes from the
// platform file or kubeconfig default.
func (s *Session) KubeContext() string { return s.kubeContext }

// Timeout returns the configured timeout for Helm operations.
func (s *Session) Timeout() time.Duration { return s.timeout }

// defaultHelmFactory creates a Helm client from the resolved destination and
// Helm runtime configuration. Destination comes from t.ClientConfig();
// storage driver, timeout, and debug come from cfg.
func defaultHelmFactory(t *target.Target, cfg HelmConfig) (HelmClient, error) {
	opts := []helm.Option{
		helm.WithRESTClientGetter(helm.NewRESTClientGetter(t.ClientConfig())),
		helm.WithNamespace(t.Namespace()),
	}
	if cfg.StorageDriver != "" {
		opts = append(opts, helm.WithStorageDriver(cfg.StorageDriver))
	}
	if cfg.Timeout > 0 {
		opts = append(opts, helm.WithTimeout(cfg.Timeout))
	}
	if cfg.Debug {
		opts = append(opts, helm.WithDebug(cfg.Debug))
	}
	return helm.NewClient(opts...)
}

// defaultKubernetesFactory creates a Kubernetes clientset from the resolved
// target's kubeconfig destination.
func defaultKubernetesFactory(t *target.Target) (kubernetes.Interface, error) {
	cfg, err := t.RESTConfig()
	if err != nil {
		return nil, fmt.Errorf("%w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
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
// Destination metadata is owned by [target.Target]. Helm construction uses
// [target.Target.ClientConfig], [HelmConfig], and [HelmFactory].
// Kubernetes construction and [Cluster.RESTConfig] use
// [target.Target.RESTConfig].
//
// Cluster does not embed or depend on [Session]. Concurrent [Cluster.Helm]
// and [Cluster.Kubernetes] calls are safe.
type Cluster struct {
	target     *target.Target
	helmConfig HelmConfig

	helmFactory HelmFactory
	k8sFactory  func(*target.Target) (kubernetes.Interface, error)

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

// Helm returns a memoized Helm client targeted at the resolved cluster.
func (cl *Cluster) Helm() (HelmClient, error) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.helm != nil {
		return cl.helm, nil
	}
	c, err := cl.helmFactory(cl.target, cl.helmConfig)
	if err != nil {
		return nil, fmt.Errorf("helm client (namespace=%q, kubeconfig=%q): %w",
			cl.target.Namespace(), cl.helmConfig.KubeconfigPath, err)
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

// RESTConfig returns a Kubernetes REST config for the resolved
// [target.Target] kubeconfig destination.
func (cl *Cluster) RESTConfig() (*rest.Config, error) {
	cfg, err := cl.target.RESTConfig()
	if err != nil {
		return nil, fmt.Errorf("%w (provide --kubeconfig or ensure KUBECONFIG/~/.kube/config is set)", err)
	}
	return cfg, nil
}

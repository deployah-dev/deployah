// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package localkube

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/sync/errgroup"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kindv1alpha4 "sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// defaultKindNodeImage is the fallback when no Kubernetes version is specified.
const defaultKindNodeImage = "kindest/node:v1.35.0"

// kindProvider implements [provider] using sigs.k8s.io/kind.
type kindProvider struct {
	p    *cluster.Provider
	cfg  *config
	sink *eventSink
	// spoolDir is the directory used for temporary image-archive files.
	// Defaults to os.TempDir() when empty.
	spoolDir string
	// resolvedRuntime is the concrete engine Kind chose; never RuntimeAuto.
	resolvedRuntime Runtime
}

// newKindProvider builds a Kind cluster.Provider wired to the given config.
func newKindProvider(cfg *config) (*kindProvider, error) {
	resolved, runtimeOpt, err := resolveKindRuntime(cfg.runtime)
	if err != nil {
		return nil, err
	}
	sink := &eventSink{fn: cfg.eventFunc, def: cfg.eventFunc}
	p := cluster.NewProvider(
		cluster.ProviderWithLogger(&slogAdapter{l: cfg.logger, onEvent: sink.emit}),
		runtimeOpt,
	)
	return &kindProvider{p: p, cfg: cfg, sink: sink, spoolDir: cfg.spoolDir, resolvedRuntime: resolved}, nil
}

// resolveKindRuntime returns the concrete Runtime and Kind ProviderOption for r.
func resolveKindRuntime(r Runtime) (Runtime, cluster.ProviderOption, error) {
	switch r {
	case RuntimeAuto:
		opt, err := cluster.DetectNodeProvider()
		if err != nil {
			return RuntimeAuto, nil, fmt.Errorf("detect container engine: %w", err)
		}
		return detectRuntimeFromOption(opt), opt, nil
	case RuntimeDocker:
		return RuntimeDocker, cluster.ProviderWithDocker(), nil
	case RuntimePodman:
		return RuntimePodman, cluster.ProviderWithPodman(), nil
	case RuntimeNerdctl:
		return RuntimeNerdctl, cluster.ProviderWithNerdctl(""), nil
	default:
		return RuntimeAuto, nil, fmt.Errorf("unsupported runtime: %v", r)
	}
}

// detectRuntimeFromOption probes which engine is available in the same order
// Kind does (docker → podman → nerdctl), falling back to RuntimeDocker.
func detectRuntimeFromOption(_ cluster.ProviderOption) Runtime {
	if isCommandAvailable("docker", "info") {
		return RuntimeDocker
	}
	if isCommandAvailable("podman", "info") {
		return RuntimePodman
	}
	if isCommandAvailable("nerdctl", "info") || isCommandAvailable("finch", "info") {
		return RuntimeNerdctl
	}
	return RuntimeDocker
}

func isCommandAvailable(cmd, arg string) bool {
	// #nosec G204 -- cmd is a hard-coded constant, not user input
	c := exec.Command(cmd, arg)
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	return c.Run() == nil
}

// kubernetesVersionToImage converts a human-supplied version string
// (e.g. "1.31" or "v1.31.2") to a kindest/node image tag.
func kubernetesVersionToImage(version string) string {
	if version == "" {
		return defaultKindNodeImage
	}
	v := strings.TrimPrefix(version, "v")
	return "kindest/node:v" + v
}

// protocolToKind maps a [Protocol] to the Kind-internal constant.
// An empty Protocol defaults to TCP so callers may omit it in a [PortMapping].
// Any other unrecognized value returns a wrapped [ErrUnsupported] error.
func protocolToKind(p Protocol) (kindv1alpha4.PortMappingProtocol, error) {
	switch p {
	case ProtocolUDP:
		return kindv1alpha4.PortMappingProtocolUDP, nil
	case ProtocolTCP, "":
		return kindv1alpha4.PortMappingProtocolTCP, nil
	default:
		return "", fmt.Errorf("%w: unsupported protocol %q (use ProtocolTCP or ProtocolUDP)", ErrUnsupported, p)
	}
}

// buildKindConfig constructs a typed v1alpha4 Cluster config from a createConfig.
// Returns an error when a PortMapping carries an unrecognized Protocol.
func buildKindConfig(cfg *createConfig) (*kindv1alpha4.Cluster, error) {
	node := kindv1alpha4.Node{Role: kindv1alpha4.ControlPlaneRole}
	for _, pm := range cfg.portMappings {
		proto, err := protocolToKind(pm.Protocol)
		if err != nil {
			return nil, err
		}
		listenAddr := pm.ListenAddress
		if listenAddr == "" {
			listenAddr = "127.0.0.1"
		}
		node.ExtraPortMappings = append(node.ExtraPortMappings, kindv1alpha4.PortMapping{
			ContainerPort: int32(pm.ContainerPort),
			HostPort:      int32(pm.HostPort),
			Protocol:      proto,
			ListenAddress: listenAddr,
		})
	}
	return &kindv1alpha4.Cluster{
		TypeMeta: kindv1alpha4.TypeMeta{
			Kind:       "Cluster",
			APIVersion: "kind.x-k8s.io/v1alpha4",
		},
		Nodes: []kindv1alpha4.Node{node},
	}, nil
}

// classifyKindErr maps Kind's untyped error strings to local sentinel errors.
// It returns (sentinel, true) when the error is recognized, or (err, false)
// when it is not.
func classifyKindErr(err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no clusters found"),
		strings.Contains(msg, "unknown cluster"),
		strings.Contains(msg, "could not locate any control plane nodes"):
		return ErrNotFound, true
	case strings.Contains(msg, "node(s) already exist for a cluster with the name"):
		return ErrAlreadyExists, true
	default:
		return err, false
	}
}

func (p *kindProvider) backendName() string { return "kind" }

func (p *kindProvider) create(ctx context.Context, name string, cfg *createConfig) (func(), error) {
	image := kubernetesVersionToImage(cfg.base.k8sVersion)

	createOpts := []cluster.CreateOption{
		cluster.CreateWithNodeImage(image),
		cluster.CreateWithRetain(cfg.retainOnFail),
		cluster.CreateWithWaitForReady(cfg.waitTimeout),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
		cluster.CreateWithKubeconfigPath(cfg.kubeconfigPath),
	}

	if len(cfg.rawKindConfig) > 0 {
		if len(cfg.portMappings) > 0 {
			p.cfg.logger.Warn("localkube: WithKindConfig takes priority; WithPortMappings is ignored")
		}
		createOpts = append(createOpts, cluster.CreateWithRawConfig(cfg.rawKindConfig))
	} else {
		kindCfg, err := buildKindConfig(cfg)
		if err != nil {
			return nil, err
		}
		createOpts = append(createOpts, cluster.CreateWithV1Alpha4Config(kindCfg))
	}

	// Kind locks <kubeconfigPath>.lock with O_CREATE|O_EXCL during create.
	// A lock left behind by a killed Kind process would make create fail with
	// "file exists", so best-effort remove a stale lock first. Only the lock
	// is touched, never the kubeconfig itself.
	os.Remove(cfg.kubeconfigPath + ".lock") //nolint:errcheck

	// Route Kind phase events through the per-call handler for the duration
	// of this create so callers can observe fine-grained progress.
	p.sink.set(cfg.emit)
	defer p.sink.reset()

	errCh := make(chan error, 1)
	go func() {
		errCh <- p.p.Create(name, createOpts...)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			if classified, ok := classifyKindErr(err); ok {
				return nil, classified
			}
			return nil, fmt.Errorf("kind create: %w", err)
		}
		return func() {}, nil
	case <-ctx.Done():
		// The create goroutine is still running; return a drain func so the
		// caller can synchronize before running cleanup (e.g. delete).
		return func() { <-errCh }, ctx.Err() //nolint:nilnil
	}
}

func (p *kindProvider) delete(_ context.Context, name string, dc *deleteConfig) error {
	// Kind locks <kubeconfigPath>.lock with O_CREATE|O_EXCL during delete.
	// A lock left behind by a killed Kind process would make delete fail with
	// "file exists", so best-effort remove a stale lock first. Only the lock
	// is touched, never the kubeconfig (Kind edits the kubeconfig on delete).
	os.Remove(dc.kubeconfigPath + ".lock") //nolint:errcheck

	// Route Kind phase events through the per-call handler for the duration
	// of this delete so callers can observe fine-grained progress.
	p.sink.set(dc.emit)
	defer p.sink.reset()

	if err := p.p.Delete(name, dc.kubeconfigPath); err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return classified
		}
		return fmt.Errorf("kind delete: %w", err)
	}
	return nil
}

func (p *kindProvider) list(_ context.Context) ([]string, error) {
	names, err := p.p.List()
	if err != nil {
		return nil, fmt.Errorf("kind list: %w", err)
	}
	return names, nil
}

func (p *kindProvider) inspect(_ context.Context, name string) (*backendInfo, error) {
	nodes, err := p.p.ListNodes(name)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return nil, classified
		}
		return nil, fmt.Errorf("kind inspect: %w", err)
	}
	// A cluster with no nodes is effectively gone: Kind still lists node
	// containers (even stopped ones) for a live cluster, so an empty list
	// means the cluster does not exist. Mirror loadImageArchive here.
	if len(nodes) == 0 {
		return nil, ErrNotFound
	}

	// Tally nodes by role. A per-node Role() error is non-fatal: the node
	// still counts toward the total, just not toward a role bucket.
	roles := make(map[string]int, 2)
	for _, n := range nodes {
		role, roleErr := n.Role()
		if roleErr != nil || role == "" {
			continue
		}
		roles[role]++
	}

	return &backendInfo{
		Nodes:   len(nodes),
		Roles:   roles,
		Runtime: p.resolvedRuntime,
	}, nil
}

func (p *kindProvider) status(ctx context.Context, name string) (Status, error) {
	raw, err := p.p.KubeConfig(name, false)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return StatusUnknown, classified
		}
		return StatusUnknown, fmt.Errorf("kind kubeconfig for status: %w", err)
	}

	restCfg, err := clientcmd.RESTConfigFromKubeConfig([]byte(raw))
	if err != nil {
		return StatusUnhealthy, fmt.Errorf("parse kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return StatusUnhealthy, fmt.Errorf("build k8s client: %w", err)
	}

	if _, verErr := client.Discovery().ServerVersion(); verErr != nil {
		if strings.Contains(verErr.Error(), "connection refused") ||
			strings.Contains(verErr.Error(), "dial tcp") {
			return StatusStopped, fmt.Errorf("API server unreachable: %w", verErr)
		}
		return StatusUnhealthy, fmt.Errorf("server version check: %w", verErr)
	}

	nodeList, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return StatusUnhealthy, fmt.Errorf("list nodes: %w", err)
	}
	for i := range nodeList.Items {
		if !isNodeReady(&nodeList.Items[i]) {
			return StatusUnhealthy, nil
		}
	}
	return StatusRunning, nil
}

func (p *kindProvider) kubeConfigBytes(_ context.Context, name string) ([]byte, error) {
	raw, err := p.p.KubeConfig(name, false)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return nil, classified
		}
		return nil, fmt.Errorf("kind kubeconfig: %w", err)
	}
	return []byte(raw), nil
}

// loadImageArchive loads a Docker/OCI tar archive into every node of the
// named cluster in parallel, with context cancellation support.
func (p *kindProvider) loadImageArchive(ctx context.Context, name string, archive io.Reader) error {
	nodes, err := p.p.ListNodes(name)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return classified
		}
		return fmt.Errorf("kind list nodes: %w", err)
	}
	if len(nodes) == 0 {
		return ErrNotFound
	}

	spoolDir := p.spoolDir
	tmp, err := os.CreateTemp(spoolDir, "localkube-image-*.tar")
	if err != nil {
		return fmt.Errorf("create temp image file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) //nolint:errcheck

	// Check cancellation before the potentially long spool copy.
	if ctxErr := ctx.Err(); ctxErr != nil {
		tmp.Close() //nolint:errcheck
		return ctxErr
	}
	if _, copyErr := io.Copy(tmp, archive); copyErr != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("buffer image archive: %w", copyErr)
	}
	if closeErr := tmp.Close(); closeErr != nil {
		return fmt.Errorf("close temp image file: %w", closeErr)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(min(len(nodes), 4))
	for _, n := range nodes {
		g.Go(func() error {
			if ctxErr := gctx.Err(); ctxErr != nil {
				return ctxErr
			}
			f, openErr := os.Open(tmpName) // #nosec G304 -- tmpName is our own temp file
			if openErr != nil {
				return openErr
			}
			loadErr := nodeutils.LoadImageArchive(n, f)
			if closeErr := f.Close(); closeErr != nil && loadErr == nil {
				return closeErr
			}
			return loadErr
		})
	}
	if waitErr := g.Wait(); waitErr != nil {
		return fmt.Errorf("load image archive to nodes: %w", waitErr)
	}
	return nil
}

// isNodeReady reports whether a Kubernetes node has the Ready condition
// set to True.
func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

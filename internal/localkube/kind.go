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

// defaultKindNodeImage is used when Kubernetes version is omitted.
const defaultKindNodeImage = "kindest/node:v1.35.0"

// kindProvider is the Kind implementation of [provider].
type kindProvider struct {
	p    *cluster.Provider
	cfg  *config
	sink *eventSink
	// spoolDir holds temp files for image archives. Empty means os.TempDir.
	spoolDir string
	// resolvedRuntime is the engine Kind actually uses. Never [RuntimeAuto].
	resolvedRuntime Runtime
}

var _ provider = (*kindProvider)(nil)

// newKindProvider returns a Kind-backed [provider] using cfg.
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

// resolveKindRuntime picks the container engine Kind will use for r.
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

// detectRuntimeFromOption picks docker, then podman, then nerdctl or finch
// from PATH. Kind's option is opaque. [RuntimeDocker] if none respond.
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

// kubernetesVersionToImage turns "1.31" or "v1.31.2" into a kindest/node tag.
// Empty uses [defaultKindNodeImage].
func kubernetesVersionToImage(version string) string {
	if version == "" {
		return defaultKindNodeImage
	}
	v := strings.TrimPrefix(version, "v")
	return "kindest/node:v" + v
}

// protocolToKind converts a [Protocol] to Kind's port-mapping constant.
// Empty means TCP. Other values return wrapped [ErrUnsupported].
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

// buildKindConfig builds a Kind cluster spec from cfg.
// Unknown [Protocol] returns wrapped [ErrUnsupported].
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
		Kind:       "Cluster",
		APIVersion: "kind.x-k8s.io/v1alpha4",
		Nodes:      []kindv1alpha4.Node{node},
	}, nil
}

// classifyKindErr maps Kind text to [ErrNotFound] or [ErrAlreadyExists].
// ok is false when unrecognized; err is unchanged.
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

// create starts a Kind cluster with the given name.
// Canceled ctx returns a wait func so delete does not race Kind's Create.
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

	// Remove a stale .lock; leftover locks make Kind fail with "file exists".
	os.Remove(cfg.kubeconfigPath + ".lock") //nolint:errcheck

	// Send Kind phase events to this call's handler.
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
		// Drain Kind's Create so delete does not race.
		return func() { <-errCh }, ctx.Err() //nolint:nilnil
	}
}

// delete removes the named Kind cluster.
func (p *kindProvider) delete(ctx context.Context, name string, dc *deleteConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Remove a stale .lock; leftover locks make Kind fail with "file exists".
	os.Remove(dc.kubeconfigPath + ".lock") //nolint:errcheck

	// Send Kind phase events to this call's handler.
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

// list returns the names of Kind clusters on this machine.
func (p *kindProvider) list(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	names, err := p.p.List()
	if err != nil {
		return nil, fmt.Errorf("kind list: %w", err)
	}
	return names, nil
}

// inspect returns node count and roles for the named cluster.
func (p *kindProvider) inspect(ctx context.Context, name string) (*backendInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	nodes, err := p.p.ListNodes(name)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return nil, classified
		}
		return nil, fmt.Errorf("kind inspect: %w", err)
	}
	// Kind lists stopped nodes, so empty means the cluster is gone.
	if len(nodes) == 0 {
		return nil, ErrNotFound
	}

	// Role() errors skip the role bucket, not the node count.
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

// status reports running, stopped, or unhealthy.
// Running means the API server answers and every node is Ready.
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

// kubeConfigBytes returns kubeconfig YAML for the named cluster.
func (p *kindProvider) kubeConfigBytes(ctx context.Context, name string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := p.p.KubeConfig(name, false)
	if err != nil {
		if classified, ok := classifyKindErr(err); ok {
			return nil, classified
		}
		return nil, fmt.Errorf("kind kubeconfig: %w", err)
	}
	return []byte(raw), nil
}

// loadImageArchive copies an image archive onto every node in the cluster.
// Canceling ctx stops waiting for remaining nodes.
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

	// Copying the archive can take a while; honor cancel first.
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

// isNodeReady reports whether node is Ready.
func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

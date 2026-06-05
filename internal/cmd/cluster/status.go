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

package cluster

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"nabat.dev/nabat"
	"nabat.dev/theme"

	"deployah.dev/deployah/internal/cli"
	"deployah.dev/deployah/internal/localkube"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// statusOptions holds command-line flags for "cluster status".
type statusOptions struct {
	OutputFormat string `nabat:"output"`
}

// accessEntry describes one externally reachable endpoint (LoadBalancer
// Service or Ingress) discovered in the cluster.
type accessEntry struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace" yaml:"namespace"`
	Name      string `json:"name" yaml:"name"`
	Host      string `json:"host,omitempty" yaml:"host,omitempty"`
	Address   string `json:"address,omitempty" yaml:"address,omitempty"`
	URL       string `json:"url,omitempty" yaml:"url,omitempty"`
	HostsLine string `json:"hostsLine,omitempty" yaml:"hostsLine,omitempty"`
	Curl      string `json:"curl,omitempty" yaml:"curl,omitempty"`
}

// clusterStatusView is the structured representation of the cluster status,
// used for JSON/YAML output and to drive the table view.
type clusterStatusView struct {
	Name                 string         `json:"name" yaml:"name"`
	Backend              string         `json:"backend" yaml:"backend"`
	Runtime              string         `json:"runtime" yaml:"runtime"`
	Status               string         `json:"status" yaml:"status"`
	Nodes                int            `json:"nodes" yaml:"nodes"`
	Roles                map[string]int `json:"roles,omitempty" yaml:"roles,omitempty"`
	Context              string         `json:"context" yaml:"context"`
	Kubeconfig           string         `json:"kubeconfig" yaml:"kubeconfig"`
	CloudProviderRunning bool           `json:"cloudProviderRunning" yaml:"cloudProviderRunning"`
	CreatedAt            string         `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	Access               []accessEntry  `json:"access,omitempty" yaml:"access,omitempty"`
}

// registerStatus attaches the "status" subcommand to the cluster group.
// app is passed so the command can read the resolved theme at run time.
func registerStatus(group *nabat.Command, app *nabat.App) {
	group.MustCommand("status",
		nabat.WithDescription("Show the local cluster status and access info"),
		nabat.WithLongDescription("Show the local cluster's health, metadata, whether the cloud provider is running, "+
			"and how to reach LoadBalancer Services and Ingresses (including suggested /etc/hosts entries)."),
		nabat.WithSelectFlag("output", cli.OutputFormatTable, cli.OutputFormats, nabat.WithShort('o'), nabat.WithUsage("Output format")),
		nabat.WithExample(`
# Show the local cluster status
deployah cluster status

# Output as JSON
deployah cluster status --output json`),
		nabat.WithRun(func(c *nabat.Context) error {
			return runStatus(c, app.Theme())
		}),
	)
}

func runStatus(c *nabat.Context, th theme.ResolvedTheme) error {
	opts := &statusOptions{}
	if err := c.Bind(opts); err != nil {
		return fmt.Errorf("binding options: %w", err)
	}

	m, err := newManager(c)
	if err != nil {
		return fmt.Errorf("init local cluster manager: %w", err)
	}

	cl, err := m.Get(c, clusterName)
	if err != nil {
		if errors.Is(err, localkube.ErrNotFound) {
			c.Info("No local cluster found", "hint", "run 'deployah cluster up' to create one")
			return nil
		}
		return fmt.Errorf("get local cluster: %w", err)
	}

	health, err := m.Status(c, clusterName)
	if err != nil {
		// Status returns a best-effort value alongside the error; log and continue.
		c.Logger().Debug("status check returned an error", "err", err)
	}

	view := clusterStatusView{
		Name:                 cl.Name,
		Backend:              cl.Backend,
		Runtime:              cl.Runtime.String(),
		Status:               health.String(),
		Nodes:                cl.Nodes,
		Roles:                cl.Roles,
		Context:              m.ContextName(clusterName),
		CloudProviderRunning: m.CloudProviderRunning(c),
	}
	if !cl.CreatedAt.IsZero() {
		view.CreatedAt = cl.CreatedAt.Format("2006-01-02 15:04:05 MST")
	}

	// Fetch the kubeconfig path and use it to discover access endpoints. Both
	// are best-effort: a stopped cluster still reports its lifecycle status.
	if kc, kcErr := m.KubeConfig(c, clusterName); kcErr == nil {
		view.Kubeconfig = kc.Path()
		view.Access = gatherAccess(c, kc.Bytes())
	} else {
		c.Logger().Debug("kubeconfig unavailable", "err", kcErr)
	}

	switch opts.OutputFormat {
	case cli.OutputFormatJSON:
		return c.JSON(view)
	case cli.OutputFormatYAML:
		return c.YAML(view)
	default:
		renderStatusSummary(c, th, view)
		return nil
	}
}

// gatherAccess lists LoadBalancer Services and Ingresses in the cluster and
// builds the externally reachable endpoints. Errors are non-fatal: an empty
// slice means nothing reachable (or the cluster is unreachable).
func gatherAccess(c *nabat.Context, kubeconfig []byte) []accessEntry {
	restCfg, err := clientcmd.RESTConfigFromKubeConfig(kubeconfig)
	if err != nil {
		c.Logger().Debug("parse kubeconfig for access info", "err", err)
		return nil
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		c.Logger().Debug("build client for access info", "err", err)
		return nil
	}

	var entries []accessEntry

	svcList, err := clientset.CoreV1().Services("").List(c, metav1.ListOptions{})
	if err != nil {
		c.Logger().Debug("list services", "err", err)
	} else {
		for i := range svcList.Items {
			svc := &svcList.Items[i]
			if svc.Spec.Type != corev1.ServiceTypeLoadBalancer {
				continue
			}
			addr := loadBalancerAddress(svc.Status.LoadBalancer.Ingress)
			if addr == "" {
				continue
			}
			entry := accessEntry{
				Kind:      "Service",
				Namespace: svc.Namespace,
				Name:      svc.Name,
				Address:   addr,
			}
			if len(svc.Spec.Ports) > 0 {
				entry.URL = fmt.Sprintf("http://%s:%d", addr, svc.Spec.Ports[0].Port)
			}
			entries = append(entries, entry)
		}
	}

	ingList, err := clientset.NetworkingV1().Ingresses("").List(c, metav1.ListOptions{})
	if err != nil {
		c.Logger().Debug("list ingresses", "err", err)
	} else {
		for i := range ingList.Items {
			ing := &ingList.Items[i]
			addr := ingressLoadBalancerAddress(ing.Status.LoadBalancer.Ingress)
			tls := hasTLS(ing)
			for _, rule := range ing.Spec.Rules {
				if rule.Host == "" {
					continue
				}
				entry := accessEntry{
					Kind:      "Ingress",
					Namespace: ing.Namespace,
					Name:      ing.Name,
					Host:      rule.Host,
					Address:   addr,
				}
				scheme := "http"
				if tls {
					scheme = "https"
				}
				entry.URL = scheme + "://" + rule.Host
				if addr != "" {
					entry.HostsLine = addr + " " + rule.Host
					entry.Curl = ingressCurl(scheme, rule.Host, addr)
				}
				entries = append(entries, entry)
			}
		}
	}

	return entries
}

// loadBalancerAddress returns the first IP (or hostname) from a Service
// LoadBalancer status ingress list, or an empty string when none is assigned.
func loadBalancerAddress(ingress []corev1.LoadBalancerIngress) string {
	for _, in := range ingress {
		if in.IP != "" {
			return in.IP
		}
		if in.Hostname != "" {
			return in.Hostname
		}
	}
	return ""
}

// ingressLoadBalancerAddress returns the first IP (or hostname) from an Ingress
// LoadBalancer status ingress list, or an empty string when none is assigned.
func ingressLoadBalancerAddress(ingress []networkingv1.IngressLoadBalancerIngress) string {
	for _, in := range ingress {
		if in.IP != "" {
			return in.IP
		}
		if in.Hostname != "" {
			return in.Hostname
		}
	}
	return ""
}

// hasTLS reports whether the Ingress declares any TLS host.
func hasTLS(ing *networkingv1.Ingress) bool {
	return len(ing.Spec.TLS) > 0
}

// ingressCurl builds a ready-to-paste curl command that reaches an ingress
// host without editing /etc/hosts. For HTTP it sends a Host header to the
// load-balancer address. For HTTPS it uses --resolve (which also sets SNI,
// required for TLS host routing) and -k to skip self-signed cert checks.
//
// When addr is a hostname rather than an IP, --resolve cannot be used, so the
// HTTPS form falls back to a plain request against the host.
func ingressCurl(scheme, host, addr string) string {
	if scheme == "https" {
		if isIPAddress(addr) {
			return fmt.Sprintf("curl -k --resolve %s:443:%s https://%s", host, addr, host)
		}
		return fmt.Sprintf("curl -k https://%s", host)
	}
	return fmt.Sprintf("curl -H 'Host: %s' http://%s", host, addr)
}

// isIPAddress reports whether s parses as an IPv4 or IPv6 address.
func isIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

// renderStatusSummary prints the cluster status and access info as human-readable
// output: a compact key/value summary (themed) followed by the access table.
func renderStatusSummary(c *nabat.Context, th theme.ResolvedTheme, view clusterStatusView) {
	// Header line: themed cluster name + colored status badge.
	c.Printf("%s   %s\n\n",
		th.Style(theme.TextTitle).Render(view.Name),
		statusBadge(th, view.Status))

	// Aligned key/value summary. Pad the label first, then style it, so the
	// ANSI escapes are not counted in the column width and alignment holds.
	label := th.Style(theme.TextMuted)
	field := func(name, value string) {
		c.Printf("  %s  %s\n", label.Render(fmt.Sprintf("%-14s", name)), value)
	}

	field("Backend", fmt.Sprintf("%s (%s)", view.Backend, view.Runtime))
	field("Nodes", nodesText(view.Nodes, view.Roles))
	field("Context", view.Context)
	field("Cloud provider", statusBadge(th, boolText(view.CloudProviderRunning, "running", "stopped")))
	if view.CreatedAt != "" {
		field("Created", view.CreatedAt)
	}
	if view.Kubeconfig != "" {
		field("Kubeconfig", shortenHome(view.Kubeconfig))
	}

	if len(view.Access) == 0 {
		return
	}

	c.Println("")
	c.Printf("%s\n", th.Style(theme.TextTitle).Render("Ingress / LoadBalancer access"))
	accessRows := make([][]string, 0, len(view.Access))
	for _, a := range view.Access {
		target := a.Host
		if target == "" {
			target = a.Name
		}
		accessRows = append(accessRows, []string{a.Kind, a.Namespace, target, a.Address, a.URL})
	}
	c.Table([]string{"KIND", "NAMESPACE", "HOST/NAME", "ADDRESS", "URL"}, accessRows, nabat.WithTableBorder(nabat.BorderRounded()))

	var hostsLines, curlLines []string
	for _, a := range view.Access {
		if a.HostsLine != "" {
			hostsLines = append(hostsLines, a.HostsLine)
		}
		if a.Curl != "" {
			curlLines = append(curlLines, a.Curl)
		}
	}
	if len(hostsLines) > 0 {
		c.Println("")
		c.Printf("%s\n", th.Style(theme.TextMuted).Render("Add these entries to /etc/hosts to resolve ingress hosts locally:"))
		c.Println("  " + strings.Join(hostsLines, "\n  "))
	}
	if len(curlLines) > 0 {
		c.Println("")
		c.Printf("%s\n", th.Style(theme.TextMuted).Render("Or test without editing /etc/hosts:"))
		c.Println("  " + strings.Join(curlLines, "\n  "))
	}
}

// boolText returns trueText when b is true, otherwise falseText.
func boolText(b bool, trueText, falseText string) string {
	if b {
		return trueText
	}
	return falseText
}

// nodesText formats the node count with an optional role breakdown, e.g.
// "3 (1 control-plane, 2 workers)". Falls back to the bare count when roles
// are unavailable. Roles are listed control-plane first, then alphabetically.
func nodesText(total int, roles map[string]int) string {
	if len(roles) == 0 {
		return strconv.Itoa(total)
	}

	names := make([]string, 0, len(roles))
	for role := range roles {
		names = append(names, role)
	}
	sort.Slice(names, func(i, j int) bool {
		// control-plane always sorts first; everything else alphabetical.
		if names[i] == "control-plane" {
			return true
		}
		if names[j] == "control-plane" {
			return false
		}
		return names[i] < names[j]
	})

	parts := make([]string, 0, len(names))
	for _, role := range names {
		parts = append(parts, fmt.Sprintf("%d %s", roles[role], pluralizeRole(role, roles[role])))
	}
	return fmt.Sprintf("%d (%s)", total, strings.Join(parts, ", "))
}

// pluralizeRole returns a human-friendly, count-aware role label, e.g.
// "control-plane" / "control-planes", "worker" / "workers".
func pluralizeRole(role string, count int) string {
	if count == 1 {
		return role
	}
	return role + "s"
}

// statusBadge renders a themed "● <status>" indicator using nabat theme tokens.
// Colors are stripped automatically by the output writer when stdout is not a
// TTY or NO_COLOR is set.
func statusBadge(th theme.ResolvedTheme, status string) string {
	var tok theme.Token
	switch status {
	case "running":
		tok = theme.StatusSuccess
	case "unhealthy":
		tok = theme.StatusWarning
	case "stopped":
		tok = theme.StatusError
	default: // "unknown" and anything else
		tok = theme.TextMuted
	}
	return th.Style(tok).Render("● " + status)
}

// shortenHome replaces the user's home directory prefix with "~" so long paths
// stay compact. Returns the input unchanged when the home dir cannot be resolved.
func shortenHome(path string) string {
	if path == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if rel, ok := strings.CutPrefix(path, home); ok {
		return "~" + rel
	}
	return path
}

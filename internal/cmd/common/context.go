package common

import (
	"fmt"

	"nabat.dev/nabat"

	"deployah.dev/deployah/internal/session"
)

// WarnContextFallback warns when cluster follows the kubeconfig
// current-context because neither --context nor a platform context named
// one, so a wrong-cluster operation is visible before it happens.
func WarnContextFallback(c *nabat.Context, cluster *session.Cluster, env string) {
	fallback, current := cluster.ContextFallback()
	if !fallback {
		return
	}

	target := "the kubeconfig's current context"
	if current != "" {
		target = fmt.Sprintf("the current kubeconfig context %q", current)
	}
	source := "no context is configured (platform file or --context)"
	if env != "" {
		source = fmt.Sprintf("environment %q has no context in the platform file and no --context was given", env)
	}
	c.Warn(source + "; using " + target)
}

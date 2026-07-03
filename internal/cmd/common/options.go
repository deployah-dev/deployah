package common

import "time"

// GlobalOptions are the persistent flags shared by all commands.
// Used exclusively in the OnPreRun hook to construct the runtime.
type GlobalOptions struct {
	Namespace    string        `nabat:"namespace"`
	Kubeconfig   string        `nabat:"kubeconfig"`
	Context      string        `nabat:"context"`
	Spec         string        `nabat:"spec"`
	PlatformFile string        `nabat:"platform-file"`
	Debug        bool          `nabat:"debug"`
	Timeout      time.Duration `nabat:"timeout"`
}

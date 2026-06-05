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
	"bytes"
	"errors"
	"io"
	"time"
)

// Cluster holds metadata about a local cluster managed via localkube.
type Cluster struct {
	// Name is the cluster name passed to Create.
	Name string
	// Backend identifies the provider; currently always "kind".
	Backend string
	// Runtime is the host container engine used for cluster nodes.
	Runtime Runtime
	// Nodes is the total node count (control-plane + workers).
	Nodes int
	// Roles counts nodes by role (e.g. "control-plane": 1, "worker": 2).
	// May be nil or incomplete if the backend cannot determine roles.
	Roles map[string]int
	// CreatedAt is the time the first node container was started.
	// May be zero if the backend cannot determine it.
	CreatedAt time.Time
}

// KubeConfig is an immutable value returned by [Manager.KubeConfig].
// All I/O (fetching from Kind and writing to disk) is done inside
// Manager.KubeConfig; the accessors on this type are side-effect-free.
type KubeConfig struct {
	path  string
	bytes []byte
}

// Bytes returns a copy of the raw kubeconfig YAML bytes.
// Callers may freely modify the returned slice without affecting this value.
func (kc *KubeConfig) Bytes() []byte { return bytes.Clone(kc.bytes) }

// Path returns the path of the kubeconfig file written by Manager.KubeConfig.
// The file is guaranteed to exist when this method is called.
func (kc *KubeConfig) Path() string { return kc.path }

// WriteTo implements [io.WriterTo]. It writes the kubeconfig YAML to w.
func (kc *KubeConfig) WriteTo(w io.Writer) (int64, error) {
	n, err := w.Write(kc.bytes)
	return int64(n), err
}

// Protocol identifies the transport-layer protocol for a port mapping.
type Protocol string

const (
	// ProtocolTCP selects TCP as the port mapping protocol. This is the default
	// when Protocol is omitted from a [PortMapping].
	ProtocolTCP Protocol = "TCP"
	// ProtocolUDP selects UDP as the port mapping protocol.
	ProtocolUDP Protocol = "UDP"
)

// PortMapping maps a host port to a container port on a cluster node.
type PortMapping struct {
	// HostPort is the port exposed on the host machine.
	HostPort uint16
	// ContainerPort is the port inside the node container.
	ContainerPort uint16
	// Protocol is [ProtocolTCP] (default) or [ProtocolUDP].
	Protocol Protocol
	// ListenAddress defaults to 127.0.0.1 when empty.
	ListenAddress string
}

// Runtime identifies the host container engine used to run cluster node
// containers.
type Runtime int

const (
	// RuntimeAuto lets Kind detect the available engine automatically.
	RuntimeAuto Runtime = iota
	// RuntimeDocker forces Docker as the host container engine.
	RuntimeDocker
	// RuntimePodman forces Podman — daemonless and rootless.
	RuntimePodman
	// RuntimeNerdctl forces nerdctl as the host container engine.
	RuntimeNerdctl
)

// String returns the runtime name ("auto", "docker", "podman", or "nerdctl").
func (r Runtime) String() string {
	switch r {
	case RuntimeDocker:
		return "docker"
	case RuntimePodman:
		return "podman"
	case RuntimeNerdctl:
		return "nerdctl"
	default:
		return "auto"
	}
}

// Backend identifies the cluster provisioning backend.
type Backend string

const (
	// BackendKind uses sigs.k8s.io/kind as the cluster backend.
	BackendKind Backend = "kind"
)

// Status reports the runtime health of a cluster.
type Status int

const (
	// StatusUnknown means the cluster state could not be determined.
	StatusUnknown Status = iota
	// StatusRunning means the API server is reachable and all nodes are Ready.
	StatusRunning
	// StatusStopped means node containers exist but are not running.
	StatusStopped
	// StatusUnhealthy means the cluster exists but the API server is
	// unreachable or one or more nodes are NotReady.
	StatusUnhealthy
)

// String returns the status name ("unknown", "running", "stopped", or
// "unhealthy").
func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	case StatusUnhealthy:
		return "unhealthy"
	default:
		return "unknown"
	}
}

// StepStatus tracks where an operation is in its lifecycle.
type StepStatus int

const (
	// StepStarted is emitted when a step begins.
	StepStarted StepStatus = iota
	// StepCompleted is emitted when a step finishes successfully.
	StepCompleted
	// StepFailed is emitted when a step fails.
	StepFailed
)

// Step is a stable identifier for a named stage in a long-running operation.
// Callers may safely switch on Step values; the closed constant set below is
// the complete list. Any value not in the list should be treated as unknown.
type Step string

// Step constants for Manager lifecycle operations.
const (
	// StepCreating is emitted by Manager.Create during cluster provisioning.
	StepCreating Step = "creating"
	// StepDeleting is emitted by Manager.Delete during cluster removal.
	StepDeleting Step = "deleting"
	// StepWritingKubeconfig is emitted by Manager.KubeConfig.
	StepWritingKubeconfig Step = "writing-kubeconfig"
	// StepLoadingImage is emitted by Manager.LoadImage / Manager.LoadImageArchive.
	StepLoadingImage Step = "loading-image"
	// StepResolvingImage is emitted during image reference resolution.
	StepResolvingImage Step = "resolving-image"

	// Cloud provider steps.

	// StepStartingCloudProvider is emitted by Manager.StartCloudProvider.
	StepStartingCloudProvider Step = "starting-cloud-provider"
	// StepStoppingCloudProvider is emitted by Manager.StopCloudProvider.
	StepStoppingCloudProvider Step = "stopping-cloud-provider"

	// Kind internal phases — emitted by the Kind backend as it provisions a cluster.
	// These are normalized from Kind's raw log lines; callers can switch on them.

	// StepPullingNodeImage is emitted by Kind while pulling the node container image.
	StepPullingNodeImage       Step = "pulling-node-image"
	StepPreparingNodes         Step = "preparing-nodes"
	StepWritingConfiguration   Step = "writing-configuration"
	StepStartingControlPlane   Step = "starting-control-plane"
	StepInstallingCNI          Step = "installing-cni"
	StepInstallingStorageClass Step = "installing-storage-class"
	StepJoiningControlPlane    Step = "joining-control-plane"
	StepJoiningWorkers         Step = "joining-workers"
	StepWaitingForReady        Step = "waiting-for-ready"
)

// Event is emitted during long-running operations to report progress.
//
// Err is set on [StepFailed] events and can be inspected with [errors.Is] or
// [errors.As]. Existing subscribers that only inspect Detail are unaffected.
type Event struct {
	// Step is one of the Step* constants.
	Step Step
	// Status indicates whether the step has started, completed, or failed.
	Status StepStatus
	// Detail carries optional free-form context (e.g. the image ref or error message).
	Detail string
	// Err is the underlying error that caused a StepFailed event, or nil.
	// Always nil for StepStarted and StepCompleted events.
	Err error
}

// EventFunc is a callback invoked with progress events.
type EventFunc func(Event)

// GatewayChannel selects which Gateway API CRDs cloud-provider-kind installs.
type GatewayChannel string

const (
	// GatewayStandard installs the stable Gateway API CRDs (default).
	GatewayStandard GatewayChannel = "standard"
	// GatewayExperimental installs the experimental Gateway API CRDs.
	GatewayExperimental GatewayChannel = "experimental"
	// GatewayDisabled skips Gateway API CRD installation entirely.
	GatewayDisabled GatewayChannel = "disabled"
)

// Sentinel errors returned by Manager methods.
var (
	// ErrNotFound is returned when a named cluster does not exist.
	ErrNotFound = errors.New("cluster not found")
	// ErrAlreadyExists is returned when Create targets a name already in use.
	ErrAlreadyExists = errors.New("cluster already exists")
	// ErrUnsupported is returned when the active backend does not support
	// the requested operation.
	ErrUnsupported = errors.New("operation not supported by this backend")
	// ErrInvalidName is returned when a cluster name is not a safe filesystem
	// component (empty, contains path separators, starts with "..", etc.).
	ErrInvalidName = errors.New("invalid cluster name")
)

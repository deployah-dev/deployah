# `localcluster` Package Design

## Overview

`localcluster` is a Go package for creating, deleting, and managing local Kubernetes clusters.
It is designed as a clean, dependency-light library importable by any Go project — not just deployah.

**Key goals:**

- Zero external dependencies in the public API (standard library only)
- Fully idiomatic Go: concrete return types, small interfaces, functional options
- Progress events decoupled from rendering — the package never owns UX
- Swappable backend (kind today, others tomorrow) without changing the public API

**Package path:** `pkg/localcluster`

---

## Design Principles

### 1. Accept interfaces, return concrete types

`New()` returns `*Manager`, not an interface. Consumers who need a mock define their own
minimal interface in their own package — exactly how `database/sql` and `net/http` work.

### 2. Layered functional options

Options exist at two levels: manager construction and per-operation. Both use the same
`func(*config)` pattern — no exported structs, no option interfaces.

### 3. Standard library only at the package boundary

Logging uses `log/slog` (`*slog.Logger`). Progress uses a plain `func(Event)` callback.
No charmbracelet, no cobra, no third-party types appear in the public API.

### 4. Typed errors, not string matching

Callers use `errors.Is` / `errors.As`. No `strings.Contains(err.Error(), "not found")`.

### 5. Backend is an implementation detail

`kind` is used internally. Its types, logger interface, and configuration never appear
in the public API. Swapping to k3d or a custom provider requires no caller changes.

---

## Public API

### Manager

```go
// Manager manages local Kubernetes clusters.
// Construct with New(); the zero value is not usable.
type Manager struct { /* unexported */ }

// New creates a Manager with the given options.
// Defaults: kind backend, no logger, defaultNodeImage, defaultTimeout.
func New(opts ...Option) (*Manager, error)

func (m *Manager) Create(ctx context.Context, name string, opts ...CreateOption) error
func (m *Manager) Delete(ctx context.Context, name string, opts ...DeleteOption) error
func (m *Manager) Get(ctx context.Context, name string) (*Cluster, error)
func (m *Manager) List(ctx context.Context) ([]*Cluster, error)
func (m *Manager) KubeConfig(ctx context.Context, name string) (string, error)
```

### Cluster

```go
type Cluster struct {
    Name string
}
```

### Events

Progress during long operations (Create, Delete) is reported through a plain callback.
The package does not render anything — that is the caller's responsibility.

```go
type StepStatus int

const (
    StepStarted   StepStatus = iota
    StepCompleted
    StepFailed
)

type Event struct {
    Step   string
    Status StepStatus
}

// EventFunc is a named type for the event callback for documentation clarity.
// WithEventHandler accepts a plain func(Event) — callers do not need to reference this type.
type EventFunc func(Event)
```

### Errors

```go
var ErrNotFound      = errors.New("cluster not found")
var ErrAlreadyExists = errors.New("cluster already exists")
```

Usage:

```go
_, err := manager.Get(ctx, "deployah")
if errors.Is(err, localcluster.ErrNotFound) {
    // cluster does not exist
}
```

---

## Options

### Manager-level options (apply to all operations)

```go
// WithLogger attaches a *slog.Logger for internal diagnostics.
// Defaults to slog.Default() if not set.
func WithLogger(l *slog.Logger) Option

// WithNodeImage pins the Kubernetes version used when creating clusters.
// Example: "kindest/node:v1.31.0"
func WithNodeImage(image string) Option

// WithTimeout sets the default timeout for all operations.
func WithTimeout(d time.Duration) Option
```

### Create options

```go
// WithEventHandler registers a callback that receives progress events during Create.
// The callback is called synchronously from the Create goroutine.
// To drive a Bubble Tea model, send events to a channel inside the callback.
func WithEventHandler(fn func(Event)) CreateOption

// WithWaitTimeout overrides the manager-level timeout for this Create call only.
func WithWaitTimeout(d time.Duration) CreateOption

// WithRetainOnFailure keeps the cluster containers alive on failure for debugging.
func WithRetainOnFailure(retain bool) CreateOption
```

### Delete options

```go
// WithForce skips the existence check and suppresses ErrNotFound.
func WithForce() DeleteOption
```

---

## Internal structure

```text
pkg/localcluster/
  manager.go      ← Manager struct, New(), public method signatures
  types.go        ← Cluster, Event, StepStatus, typed errors
  options.go      ← Option / CreateOption / DeleteOption constructors
  config.go       ← unexported config / createConfig / deleteConfig structs
  provider.go     ← unexported Provider interface
  kind.go         ← kindProvider: implements Provider, adapts kind's Logger interface
  defaults.go     ← defaultNodeImage, defaultTimeout constants
```

The `Provider` interface is unexported. It exists solely to allow the kind backend to be
swapped in tests without exposing backend details to callers.

```go
// unexported — internal to the package
type provider interface {
    create(ctx context.Context, name string, cfg *createConfig) error
    delete(ctx context.Context, name string, cfg *deleteConfig) error
    list(ctx context.Context) ([]*Cluster, error)
    kubeConfig(ctx context.Context, name string) (string, error)
}
```

---

## Usage examples

### Minimal — zero config

```go
m, err := localcluster.New()
if err != nil {
    log.Fatal(err)
}

if err := m.Create(context.Background(), "deployah"); err != nil {
    log.Fatal(err)
}
```

### With logger and progress callback

```go
logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

m, err := localcluster.New(
    localcluster.WithLogger(logger),
    localcluster.WithNodeImage("kindest/node:v1.31.0"),
    localcluster.WithTimeout(5 * time.Minute),
)
if err != nil {
    log.Fatal(err)
}

err = m.Create(context.Background(), "deployah",
    localcluster.WithEventHandler(func(e localcluster.Event) {
        switch e.Status {
        case localcluster.StepStarted:
            fmt.Printf("  → %s\n", e.Step)
        case localcluster.StepCompleted:
            fmt.Printf("  ✓ %s\n", e.Step)
        case localcluster.StepFailed:
            fmt.Printf("  ✗ %s\n", e.Step)
        }
    }),
)
```

### Driving a Bubble Tea model from the callback

```go
events := make(chan localcluster.Event, 16)

go func() {
    err = m.Create(ctx, "deployah",
        localcluster.WithEventHandler(func(e localcluster.Event) {
            events <- e
        }),
    )
    close(events)
}()

// Bubble Tea program ranges over events and updates model state.
```

### Error handling

```go
err := m.Create(ctx, "deployah")
if errors.Is(err, localcluster.ErrAlreadyExists) {
    // already running — not fatal
}

cluster, err := m.Get(ctx, "deployah")
if errors.Is(err, localcluster.ErrNotFound) {
    // not created yet
}
```

### In tests — record events without rendering

```go
var recorded []localcluster.Event

err := m.Create(ctx, "test-cluster",
    localcluster.WithEventHandler(func(e localcluster.Event) {
        recorded = append(recorded, e)
    }),
)

assert.NoError(t, err)
assert.Equal(t, localcluster.StepCompleted, recorded[len(recorded)-1].Status)
```

---

## What this design intentionally excludes

| Concern | Decision |
|---|---|
| Rendering / TUI | Caller's responsibility via `func(Event)` |
| CLI flags | Caller's responsibility — no cobra dependency |
| kubeconfig merging into `~/.kube/config` | Out of scope for v1; return raw string via `KubeConfig()` |
| Multi-node clusters | Out of scope for v1; can be added as `CreateOption` later |
| Provider selection by caller | Unexported — backend is an implementation detail |

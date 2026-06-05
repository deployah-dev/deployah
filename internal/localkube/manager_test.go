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
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeProvider records calls and returns configured responses.
type fakeProvider struct {
	mu sync.Mutex

	createErr        error
	deleteErr        error
	listResult       []string
	listErr          error
	inspectResult    *backendInfo
	inspectErr       error
	statusResult     Status
	statusErr        error
	kubeconfigResult []byte
	kubeconfigErr    error
	loadArchiveErr   error

	createCalls int
	deleteCalls int
	loadCalls   int
	lastArchive []byte // contents read from last loadImageArchive call

	// createBlockCh, when non-nil, causes the wait func returned by create()
	// to block until the channel is closed, mimicking a slow Kind goroutine.
	createBlockCh chan struct{}
	// backendNameOverride overrides the value returned by backendName().
	backendNameOverride string
	// activeLoads records the number of concurrent loadImageArchive goroutines.
	activeLoads     int
	peakActiveLoads int
}

func (f *fakeProvider) backendName() string {
	if f.backendNameOverride != "" {
		return f.backendNameOverride
	}
	return "kind"
}

func (f *fakeProvider) create(_ context.Context, _ string, _ *createConfig) (func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createCalls++

	blockCh := f.createBlockCh
	wait := func() {
		if blockCh != nil {
			<-blockCh
		}
	}
	if f.createErr != nil {
		return wait, f.createErr //nolint:nilnil
	}
	return wait, nil
}

func (f *fakeProvider) delete(_ context.Context, _ string, _ *deleteConfig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deleteCalls++
	return f.deleteErr
}

func (f *fakeProvider) list(_ context.Context) ([]string, error) {
	return f.listResult, f.listErr
}

func (f *fakeProvider) inspect(_ context.Context, _ string) (*backendInfo, error) {
	if f.inspectErr != nil {
		return nil, f.inspectErr
	}
	if f.inspectResult != nil {
		return f.inspectResult, nil
	}
	return &backendInfo{Nodes: 1, Runtime: RuntimeDocker}, nil
}

func (f *fakeProvider) status(_ context.Context, _ string) (Status, error) {
	return f.statusResult, f.statusErr
}

func (f *fakeProvider) kubeConfigBytes(_ context.Context, _ string) ([]byte, error) {
	if f.kubeconfigErr != nil {
		return nil, f.kubeconfigErr
	}
	if f.kubeconfigResult != nil {
		return f.kubeconfigResult, nil
	}
	return []byte("apiVersion: v1\nkind: Config\n"), nil
}

func (f *fakeProvider) loadImageArchive(_ context.Context, _ string, archive io.Reader) error {
	f.mu.Lock()
	f.loadCalls++
	f.activeLoads++
	if f.activeLoads > f.peakActiveLoads {
		f.peakActiveLoads = f.activeLoads
	}
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		f.activeLoads--
		f.mu.Unlock()
	}()

	if f.loadArchiveErr != nil {
		return f.loadArchiveErr
	}
	data, readErr := io.ReadAll(archive)
	if readErr != nil {
		return readErr
	}
	f.mu.Lock()
	f.lastArchive = data
	f.mu.Unlock()
	return nil
}

// newTestManager creates a Manager backed by a fakeProvider and a temp
// kubeconfig dir.
func newTestManager(t *testing.T, fp *fakeProvider) *Manager {
	t.Helper()
	dir := t.TempDir()
	kcs, err := newKubeconfigStore(dir)
	require.NoError(t, err)
	cfg := &config{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		timeout:       10 * time.Second,
		runtime:       RuntimeAuto,
		eventFunc:     func(Event) {},
		kubeconfigDir: dir,
	}
	return &Manager{cfg: cfg, prov: fp, kcs: kcs}
}

// TestCreate_success invokes the provider's create and emits the right events.
func TestCreate_success(t *testing.T) {
	fp := &fakeProvider{}
	m := newTestManager(t, fp)

	err := m.Create(context.Background(), "mycluster")
	require.NoError(t, err)
	assert.Equal(t, 1, fp.createCalls)
}

// TestCreate_alreadyExists_withoutFlag returns ErrAlreadyExists.
func TestCreate_alreadyExists_withoutFlag(t *testing.T) {
	fp := &fakeProvider{createErr: ErrAlreadyExists}
	m := newTestManager(t, fp)

	err := m.Create(context.Background(), "mycluster")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrAlreadyExists))
}

// TestCreate_alreadyExists_withCreateIfMissing treats an existing cluster
// as success.
func TestCreate_alreadyExists_withCreateIfMissing(t *testing.T) {
	fp := &fakeProvider{createErr: ErrAlreadyExists}
	m := newTestManager(t, fp)

	err := m.Create(context.Background(), "mycluster", WithCreateIfMissing())
	require.NoError(t, err)
}

// TestCreate_emitsEvents emits started and completed events on success.
func TestCreate_emitsEvents(t *testing.T) {
	fp := &fakeProvider{}
	var events []Event
	m := newTestManager(t, fp)
	m.cfg.eventFunc = func(e Event) { events = append(events, e) }

	require.NoError(t, m.Create(context.Background(), "dev"))

	require.Len(t, events, 2)
	assert.Equal(t, StepCreating, events[0].Step)
	assert.Equal(t, StepStarted, events[0].Status)
	assert.Equal(t, StepCreating, events[1].Step)
	assert.Equal(t, StepCompleted, events[1].Status)
}

// TestCreate_emitsFailed_onError emits a failed event when create fails.
func TestCreate_emitsFailed_onError(t *testing.T) {
	createErr := errors.New("boom")
	fp := &fakeProvider{createErr: createErr}
	var events []Event
	m := newTestManager(t, fp)
	m.cfg.eventFunc = func(e Event) { events = append(events, e) }

	err := m.Create(context.Background(), "dev")
	require.Error(t, err)

	require.Len(t, events, 2)
	assert.Equal(t, StepFailed, events[1].Status)
	assert.ErrorIs(t, events[1].Err, createErr, "Event.Err should wrap the provider error")
	assert.Equal(t, createErr.Error(), events[1].Detail)
}

// TestCreate_canceled_emitsFailedEvent asserts that a StepFailed event is
// emitted when the context is canceled during Create.
func TestCreate_canceled_emitsFailedEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already canceled

	fp := &fakeProvider{createErr: context.Canceled}
	var events []Event
	m := newTestManager(t, fp)
	m.cfg.eventFunc = func(e Event) { events = append(events, e) }

	err := m.Create(ctx, "dev")
	require.Error(t, err)
	require.True(t, errors.Is(err, context.Canceled))

	require.Len(t, events, 2, "expected StepStarted + StepFailed")
	assert.Equal(t, StepStarted, events[0].Status)
	assert.Equal(t, StepFailed, events[1].Status)
}

// TestCreate_canceled_waitsForProviderBeforeCleanup asserts that the cleanup
// goroutine does not call delete until the provider's wait function unblocks.
func TestCreate_canceled_waitsForProviderBeforeCleanup(t *testing.T) {
	blockCh := make(chan struct{})
	fp := &fakeProvider{
		createErr:     context.Canceled,
		createBlockCh: blockCh,
		deleteErr:     nil,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := newTestManager(t, fp)

	err := m.Create(ctx, "dev")
	require.Error(t, err)

	// Immediately after Create returns the delete goroutine must NOT have run
	// yet because blockCh is still open.
	fp.mu.Lock()
	deleteBefore := fp.deleteCalls
	fp.mu.Unlock()
	assert.Equal(t, 0, deleteBefore, "delete must not be called before wait unblocks")

	// Unblock the simulated Kind goroutine.
	close(blockCh)

	// Give the cleanup goroutine a moment to run.
	assert.Eventually(t, func() bool {
		fp.mu.Lock()
		defer fp.mu.Unlock()
		return fp.deleteCalls == 1
	}, 2*time.Second, 10*time.Millisecond, "cleanup delete should be called after wait unblocks")
}

// TestDelete_success calls the provider's delete.
func TestDelete_success(t *testing.T) {
	fp := &fakeProvider{}
	m := newTestManager(t, fp)

	err := m.Delete(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, fp.deleteCalls)
}

// TestDelete_notFound_withoutFlag returns ErrNotFound when the cluster is missing.
func TestDelete_notFound_withoutFlag(t *testing.T) {
	fp := &fakeProvider{deleteErr: ErrNotFound}
	m := newTestManager(t, fp)

	err := m.Delete(context.Background(), "dev")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

// TestDelete_notFound_withIgnoreMissing succeeds when the cluster is missing.
func TestDelete_notFound_withIgnoreMissing(t *testing.T) {
	fp := &fakeProvider{deleteErr: ErrNotFound}
	m := newTestManager(t, fp)

	err := m.Delete(context.Background(), "dev", WithIgnoreMissing())
	require.NoError(t, err)
}

// TestDeleteEventHandler_fansOut verifies that both the per-call and the
// manager-level handlers receive every event (fan-out, not override).
func TestDeleteEventHandler_fansOut(t *testing.T) {
	fp := &fakeProvider{}
	var mgr []Event
	m := newTestManager(t, fp)
	m.cfg.eventFunc = func(e Event) { mgr = append(mgr, e) }

	var perCall []Event
	err := m.Delete(context.Background(), "dev",
		WithDeleteEventHandler(func(e Event) { perCall = append(perCall, e) }))
	require.NoError(t, err)

	require.Len(t, perCall, 2, "per-call handler must receive both events")
	assert.Equal(t, StepStarted, perCall[0].Status)
	assert.Equal(t, StepCompleted, perCall[1].Status)
	require.Len(t, mgr, 2, "manager-level handler must also receive both events (fan-out)")
	assert.Equal(t, StepStarted, mgr[0].Status)
	assert.Equal(t, StepCompleted, mgr[1].Status)
}

// TestRecreate deletes an existing cluster and creates a fresh one.
func TestRecreate(t *testing.T) {
	fp := &fakeProvider{}
	m := newTestManager(t, fp)

	err := m.Recreate(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, fp.deleteCalls)
	assert.Equal(t, 1, fp.createCalls)
}

// TestRecreate_survivesClusterNotExisting succeeds when delete finds no cluster.
func TestRecreate_survivesClusterNotExisting(t *testing.T) {
	fp := &fakeProvider{deleteErr: ErrNotFound}
	m := newTestManager(t, fp)

	err := m.Recreate(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, 1, fp.createCalls)
}

// TestGet_notFound returns ErrNotFound when the provider does not know
// the cluster.
func TestGet_notFound(t *testing.T) {
	fp := &fakeProvider{inspectErr: ErrNotFound}
	m := newTestManager(t, fp)

	_, err := m.Get(context.Background(), "unknown")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

// TestGet_success returns cluster metadata from the backend.
func TestGet_success(t *testing.T) {
	fp := &fakeProvider{inspectResult: &backendInfo{
		Nodes:   3,
		Roles:   map[string]int{"control-plane": 1, "worker": 2},
		Runtime: RuntimePodman,
	}}
	m := newTestManager(t, fp)

	c, err := m.Get(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", c.Name)
	assert.Equal(t, 3, c.Nodes)
	assert.Equal(t, map[string]int{"control-plane": 1, "worker": 2}, c.Roles)
	assert.Equal(t, RuntimePodman, c.Runtime)
}

// TestList_respectsManagerTimeout verifies that List cancels when the manager
// timeout is exceeded.
func TestList_respectsManagerTimeout(t *testing.T) {
	blockCh := make(chan struct{})
	t.Cleanup(func() { close(blockCh) })

	m := newTestManager(t, &fakeProvider{})
	m.cfg.timeout = 50 * time.Millisecond
	m.prov = &blockingListProvider{blockCh: blockCh, fakeProvider: &fakeProvider{}}

	_, err := m.List(context.Background())
	require.Error(t, err)
	assert.True(t, errors.Is(err, context.DeadlineExceeded))
}

// blockingListProvider wraps fakeProvider and blocks list until blockCh closes.
type blockingListProvider struct {
	*fakeProvider
	blockCh <-chan struct{}
}

func (b *blockingListProvider) list(ctx context.Context) ([]string, error) {
	select {
	case <-b.blockCh:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestList_sortedByName verifies that List returns clusters in
// lexicographic order.
func TestList_sortedByName(t *testing.T) {
	fp := &fakeProvider{listResult: []string{"charlie", "alice", "bob"}, inspectResult: &backendInfo{Nodes: 1}}
	m := newTestManager(t, fp)

	clusters, err := m.List(context.Background())
	require.NoError(t, err)
	require.Len(t, clusters, 3)
	assert.Equal(t, "alice", clusters[0].Name)
	assert.Equal(t, "bob", clusters[1].Name)
	assert.Equal(t, "charlie", clusters[2].Name)
}

// TestStatus_propagatesProviderError verifies that a non-nil error from the
// provider is surfaced.
func TestStatus_propagatesProviderError(t *testing.T) {
	provErr := errors.New("api server unreachable")
	fp := &fakeProvider{statusResult: StatusStopped, statusErr: provErr}
	m := newTestManager(t, fp)

	status, err := m.Status(context.Background(), "dev")
	require.Error(t, err)
	assert.ErrorIs(t, err, provErr)
	assert.Equal(t, StatusStopped, status)
}

// TestStatus_returnsRunningWithNoError verifies the happy path.
func TestStatus_returnsRunningWithNoError(t *testing.T) {
	fp := &fakeProvider{statusResult: StatusRunning}
	m := newTestManager(t, fp)

	status, err := m.Status(context.Background(), "dev")
	require.NoError(t, err)
	assert.Equal(t, StatusRunning, status)
}

// TestKubeConfig_writesFileAndReturnsStruct writes kubeconfig bytes and
// exposes accessors.
func TestKubeConfig_writesFileAndReturnsStruct(t *testing.T) {
	raw := []byte("apiVersion: v1\nkind: Config\nclusters: []\n")
	fp := &fakeProvider{kubeconfigResult: raw}
	m := newTestManager(t, fp)

	kc, err := m.KubeConfig(context.Background(), "dev")
	require.NoError(t, err)

	assert.Equal(t, raw, kc.Bytes())
	assert.NotEmpty(t, kc.Path())

	var buf bytes.Buffer
	_, err = kc.WriteTo(&buf)
	require.NoError(t, err)
	assert.Equal(t, raw, buf.Bytes())
}

// TestKubeConfig_BytesIsCopy verifies that mutating the returned slice does
// not affect subsequent calls to Bytes().
func TestKubeConfig_BytesIsCopy(t *testing.T) {
	raw := []byte("apiVersion: v1\nkind: Config\nclusters: []\n")
	fp := &fakeProvider{kubeconfigResult: raw}
	m := newTestManager(t, fp)

	kc, err := m.KubeConfig(context.Background(), "dev")
	require.NoError(t, err)

	b1 := kc.Bytes()
	b1[0] = 'X' // mutate the first copy

	b2 := kc.Bytes()
	assert.Equal(t, raw, b2, "Bytes() should return an independent copy each time")
}

// TestContextName returns the Kind context name for a cluster.
func TestContextName(t *testing.T) {
	m := newTestManager(t, &fakeProvider{})
	assert.Equal(t, "kind-mycluster", m.ContextName("mycluster"))
}

// TestLoadImageArchive_delegatesToProvider forwards the archive to the backend.
func TestLoadImageArchive_delegatesToProvider(t *testing.T) {
	fp := &fakeProvider{}
	m := newTestManager(t, fp)

	payload := []byte("fake-image-bytes")
	err := m.LoadImageArchive(context.Background(), "dev", bytes.NewReader(payload))
	require.NoError(t, err)

	assert.Equal(t, 1, fp.loadCalls)
	assert.Equal(t, payload, fp.lastArchive)
}

// TestWithSpoolDir verifies that WithSpoolDir stores the directory in config.
func TestWithSpoolDir(t *testing.T) {
	customDir := t.TempDir()
	fp := &fakeProvider{}
	m := newTestManager(t, fp)
	WithSpoolDir(customDir)(m.cfg)
	assert.Equal(t, customDir, m.cfg.spoolDir)
}

// TestInvalidClusterName_allEntryPoints asserts that every public Manager
// method that accepts a cluster name rejects unsafe names with ErrInvalidName.
func TestInvalidClusterName_allEntryPoints(t *testing.T) {
	badNames := []string{"", "..", "../x", "a/b"}
	fp := &fakeProvider{}
	m := newTestManager(t, fp)
	ctx := context.Background()

	for _, name := range badNames {
		t.Run(name, func(t *testing.T) {
			assert.ErrorIs(t, m.Create(ctx, name), ErrInvalidName)
			assert.ErrorIs(t, m.Delete(ctx, name), ErrInvalidName)
			_, err := m.Get(ctx, name)
			assert.ErrorIs(t, err, ErrInvalidName)
			_, err = m.Status(ctx, name)
			assert.ErrorIs(t, err, ErrInvalidName)
			_, err = m.KubeConfig(ctx, name)
			assert.ErrorIs(t, err, ErrInvalidName)
			assert.ErrorIs(t, m.LoadImage(ctx, name, "ubuntu:latest"), ErrInvalidName)
			assert.ErrorIs(t, m.LoadImageArchive(ctx, name, bytes.NewReader(nil)), ErrInvalidName)
		})
	}
}

// TestClose_waitsForBackgroundCleanup asserts Close blocks until cleanup
// goroutines finish.
func TestClose_waitsForBackgroundCleanup(t *testing.T) {
	blockCh := make(chan struct{})
	fp := &fakeProvider{
		createErr:     context.Canceled,
		createBlockCh: blockCh,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := newTestManager(t, fp)
	require.Error(t, m.Create(ctx, "dev"))

	// Close should not return until the cleanup goroutine finishes.
	done := make(chan struct{})
	go func() {
		require.NoError(t, m.Close())
		close(done)
	}()

	// Close is blocking on the cleanup goroutine which is waiting on blockCh.
	select {
	case <-done:
		t.Fatal("Close returned before cleanup goroutine unblocked")
	case <-time.After(50 * time.Millisecond):
	}

	close(blockCh)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after cleanup goroutine unblocked")
	}
}

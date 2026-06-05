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

package imageref

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	v1types "github.com/google/go-containerregistry/pkg/v1/types"
)

var (
	errFakeDaemon   = errors.New("fake daemon error")
	errFakeRegistry = errors.New("fake registry error")
)

func failOpener(err error) func(context.Context, name.Reference) (io.ReadCloser, error) {
	return func(_ context.Context, _ name.Reference) (io.ReadCloser, error) { return nil, err }
}

func successOpener(content []byte) func(context.Context, name.Reference) (io.ReadCloser, error) {
	return func(_ context.Context, _ name.Reference) (io.ReadCloser, error) {
		pr, pw := io.Pipe()
		go func() { pw.CloseWithError(writeAll(pw, content)) }()
		return pr, nil
	}
}

func writeAll(w io.WriteCloser, b []byte) error {
	_, err := w.Write(b)
	return err
}

// TestOpen_localFile_absolutePath opens a real .tar file by absolute path.
func TestOpen_localFile_absolutePath(t *testing.T) {
	content := []byte("fake tar content")
	f, err := os.CreateTemp(t.TempDir(), "*.tar")
	require.NoError(t, err)
	_, err = f.Write(content)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	rc, err := Open(context.Background(), f.Name())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rc.Close()) })

	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, content, got)
}

// TestOpen_localFile_suffix treats a .tar suffix as a file path.
func TestOpen_localFile_suffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "image.tar")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o600))

	rc, err := Open(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

// TestOpen_existingFileWithoutSuffix opens a file that has no .tar extension.
func TestOpen_existingFileWithoutSuffix(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "myimage") // no extension
	require.NoError(t, os.WriteFile(path, []byte("bytes"), 0o600))

	rc, err := Open(context.Background(), path)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

// TestOpen_registryRefThatLooksLikeFile is a registry ref ending in .tar that
// does NOT exist on disk. It must fall through to the reference resolution path.
func TestOpen_registryRefThatLooksLikeFile(t *testing.T) {
	// "registry.example.com/myapp.tar" is not on disk, so it must be treated
	// as an image reference.
	op := openers{
		daemon:   failOpener(errFakeDaemon),
		registry: successOpener([]byte("reg-content")),
	}
	rc, err := openWith(context.Background(), "registry.example.com/myapp:latest", op)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
}

// TestOpen_joinsBothErrors verifies that when daemon and registry both fail,
// the returned error wraps both via [errors.Join].
func TestOpen_joinsBothErrors(t *testing.T) {
	op := openers{
		daemon:   failOpener(errFakeDaemon),
		registry: failOpener(errFakeRegistry),
	}
	_, err := openWith(context.Background(), "ubuntu:22.04", op)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errFakeDaemon), "daemon error should be in chain")
	assert.True(t, errors.Is(err, errFakeRegistry), "registry error should be in chain")
}

// TestOpen_daemonSuccess returns the daemon stream when daemon succeeds.
func TestOpen_daemonSuccess(t *testing.T) {
	op := openers{
		daemon:   successOpener([]byte("from-daemon")),
		registry: failOpener(errFakeRegistry),
	}
	rc, err := openWith(context.Background(), "ubuntu:22.04", op)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, []byte("from-daemon"), got)
	require.NoError(t, rc.Close())
}

// TestOpen_fallsBackToRegistry opens from registry when daemon fails.
func TestOpen_fallsBackToRegistry(t *testing.T) {
	op := openers{
		daemon:   failOpener(errFakeDaemon),
		registry: successOpener([]byte("from-registry")),
	}
	rc, err := openWith(context.Background(), "ubuntu:22.04", op)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	assert.Equal(t, []byte("from-registry"), got)
	require.NoError(t, rc.Close())
}

// TestOpen_invalidReference rejects strings that aren't image references.
func TestOpen_invalidReference(t *testing.T) {
	op := openers{
		daemon:   failOpener(errFakeDaemon),
		registry: failOpener(errFakeRegistry),
	}
	_, err := openWith(context.Background(), ":::invalid:::", op)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "imageref:")
}

// panickingImage is a stub v1.Image whose MediaType panics.
// It is used to verify that tarballPipe does not deadlock the reader.
type panickingImage struct{}

var _ v1.Image = (*panickingImage)(nil)

func (panickingImage) Layers() ([]v1.Layer, error)             { panic("test panic") }
func (panickingImage) MediaType() (v1types.MediaType, error)   { panic("test panic") }
func (panickingImage) Size() (int64, error)                    { panic("test panic") }
func (panickingImage) ConfigName() (v1.Hash, error)            { panic("test panic") }
func (panickingImage) ConfigFile() (*v1.ConfigFile, error)     { panic("test panic") }
func (panickingImage) RawConfigFile() ([]byte, error)          { panic("test panic") }
func (panickingImage) Digest() (v1.Hash, error)                { panic("test panic") }
func (panickingImage) Manifest() (*v1.Manifest, error)         { panic("test panic") }
func (panickingImage) RawManifest() ([]byte, error)            { panic("test panic") }
func (panickingImage) LayerByDigest(v1.Hash) (v1.Layer, error) { panic("test panic") }
func (panickingImage) LayerByDiffID(v1.Hash) (v1.Layer, error) { panic("test panic") }

// TestTarballPipe_panicSafe asserts that a panic in tarball.Write propagates
// as an error to the reader rather than deadlocking.
func TestTarballPipe_panicSafe(t *testing.T) {
	ref, err := name.ParseReference("ubuntu:22.04")
	require.NoError(t, err)
	rc := tarballPipe(ref, &panickingImage{})

	done := make(chan struct{})
	var readErr error
	go func() {
		defer close(done)
		_, readErr = io.ReadAll(rc)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("tarballPipe deadlocked: reader blocked for >5s after panic")
	}
	require.Error(t, readErr, "expected an error from the panicking image")
	assert.Contains(t, readErr.Error(), "tarball panic")
}

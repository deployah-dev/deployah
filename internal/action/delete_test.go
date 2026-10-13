package action_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockDeleter struct {
	release     *v1.Release
	getErr      error
	delErr      error
	gotProject  string
	gotEnv      string
	gotWait     bool
	getCalls    int
	deleteCalls int
	ctx         context.Context
}

func (m *mockDeleter) GetRelease(ctx context.Context, project, environment string) (*v1.Release, error) {
	m.getCalls++
	m.ctx = ctx
	m.gotProject = project
	m.gotEnv = environment
	if m.getErr != nil {
		return nil, m.getErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.release, nil
}

func (m *mockDeleter) DeleteRelease(ctx context.Context, project, environment string, wait bool) error {
	m.deleteCalls++
	m.ctx = ctx
	m.gotProject = project
	m.gotEnv = environment
	m.gotWait = wait
	if m.delErr != nil {
		return m.delErr
	}
	return ctx.Err()
}

// Check returns ErrReleaseNotFound when the release is missing and yes is false.
func TestDelete_Check_NotFoundWithoutYes(t *testing.T) {
	t.Parallel()
	m := &mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)}
	del := action.NewDelete(m)
	result, err := del.Check(t.Context(), action.DeleteParams{Project: "x", Environment: "prod", Yes: false})
	require.ErrorIs(t, err, helm.ErrReleaseNotFound)
	assert.ErrorContains(t, err, "project 'x' in environment 'prod'")
	assert.Nil(t, result)
	assert.Equal(t, "x", m.gotProject)
	assert.Equal(t, "prod", m.gotEnv)
	assert.Equal(t, 1, m.getCalls)
	assert.Zero(t, m.deleteCalls)
}

// Check succeeds with NotFound when the release is missing and yes is true.
func TestDelete_Check_NotFoundWithYes(t *testing.T) {
	t.Parallel()
	m := &mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)}
	del := action.NewDelete(m)
	result, err := del.Check(t.Context(), action.DeleteParams{Project: "x", Environment: "prod", Yes: true})
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.NotFound)
	assert.Nil(t, result.Release)
	assert.Equal(t, "x", m.gotProject)
	assert.Equal(t, "prod", m.gotEnv)
	assert.Zero(t, m.deleteCalls)
}

// Check returns the release when it exists, whether or not yes is set.
func TestDelete_Check_Found(t *testing.T) {
	t.Parallel()
	rel := &v1.Release{Name: "x-prod"}
	tests := []struct {
		name string
		yes  bool
	}{
		{name: "without yes", yes: false},
		{name: "yes still returns the release", yes: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockDeleter{release: rel}
			del := action.NewDelete(m)
			result, err := del.Check(t.Context(), action.DeleteParams{
				Project: "x", Environment: "prod", Yes: tt.yes,
			})
			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Equal(t, rel, result.Release)
			assert.False(t, result.NotFound)
			assert.Equal(t, "x", m.gotProject)
			assert.Equal(t, "prod", m.gotEnv)
			assert.Equal(t, 1, m.getCalls)
			assert.Zero(t, m.deleteCalls)
		})
	}
}

// Check wraps lookup errors that are not a missing release.
func TestDelete_Check_OtherGetError(t *testing.T) {
	t.Parallel()
	errGet := errors.New("api unavailable")
	tests := []struct {
		name string
		yes  bool
	}{
		{name: "without yes", yes: false},
		{name: "yes does not skip other errors", yes: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := &mockDeleter{getErr: errGet}
			del := action.NewDelete(m)
			result, err := del.Check(t.Context(), action.DeleteParams{
				Project: "shop", Environment: "prod", Yes: tt.yes,
			})
			require.ErrorIs(t, err, errGet)
			assert.ErrorContains(t, err, "check project status")
			assert.Nil(t, result)
			assert.Equal(t, "shop", m.gotProject)
			assert.Equal(t, "prod", m.gotEnv)
			assert.Zero(t, m.deleteCalls)
		})
	}
}

// Check returns a canceled lookup without deleting.
func TestDelete_Check_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m := &mockDeleter{}
	del := action.NewDelete(m)
	result, err := del.Check(ctx, action.DeleteParams{Project: "shop", Environment: "prod"})
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "check project status")
	assert.Nil(t, result)
	assert.Zero(t, m.deleteCalls)
}

// Execute deletes the release successfully.
func TestDelete_Execute_Success(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(t.Context(), ctxMarker{}, "delete")
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(ctx, "x", "prod", false)
	require.NoError(t, err)
	assert.Equal(t, "x", m.gotProject)
	assert.Equal(t, "prod", m.gotEnv)
	assert.Equal(t, "delete", m.ctx.Value(ctxMarker{}))
	assert.Equal(t, 1, m.deleteCalls)
	assert.Zero(t, m.getCalls)
}

// Execute wraps deleter errors.
func TestDelete_Execute_Error(t *testing.T) {
	t.Parallel()
	errDelete := errors.New("helm delete failed")
	m := &mockDeleter{delErr: errDelete}
	del := action.NewDelete(m)
	err := del.Execute(t.Context(), "x", "prod", false)
	require.ErrorIs(t, err, errDelete)
	assert.ErrorContains(t, err, "delete release")
	assert.Equal(t, "x", m.gotProject)
	assert.Equal(t, "prod", m.gotEnv)
	assert.Zero(t, m.getCalls)
}

// Execute returns a canceled delete.
func TestDelete_Execute_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(ctx, "shop", "prod", true)
	require.ErrorIs(t, err, context.Canceled)
	assert.ErrorContains(t, err, "delete release")
	assert.Equal(t, "shop", m.gotProject)
	assert.Equal(t, "prod", m.gotEnv)
	assert.True(t, m.gotWait)
	assert.Zero(t, m.getCalls)
}

// Execute passes wait=false to the deleter by default.
func TestDelete_Execute_WaitFalse(t *testing.T) {
	t.Parallel()
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(t.Context(), "x", "prod", false)
	require.NoError(t, err)
	assert.False(t, m.gotWait)
}

// Execute passes wait=true to the deleter when requested.
func TestDelete_Execute_WaitTrue(t *testing.T) {
	t.Parallel()
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(t.Context(), "x", "prod", true)
	require.NoError(t, err)
	assert.True(t, m.gotWait)
}

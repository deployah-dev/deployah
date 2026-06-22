package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"deployah.dev/deployah/internal/action"
	"deployah.dev/deployah/internal/helm"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockDeleter struct {
	release *v1.Release
	getErr  error
	delErr  error
	gotWait bool
}

func (m *mockDeleter) GetRelease(_ context.Context, _, _ string) (*v1.Release, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.release, nil
}

func (m *mockDeleter) DeleteRelease(_ context.Context, _, _ string, wait bool) error {
	m.gotWait = wait
	return m.delErr
}

// Check returns ErrReleaseNotFound when the release is missing and yes is false.
func TestDelete_Check_NotFoundWithoutYes(t *testing.T) {
	del := action.NewDelete(&mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)})
	_, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod", Yes: false})
	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrReleaseNotFound)
}

// Check succeeds with NotFound when the release is missing and yes is true.
func TestDelete_Check_NotFoundWithYes(t *testing.T) {
	del := action.NewDelete(&mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)})
	result, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod", Yes: true})
	require.NoError(t, err)
	assert.True(t, result.NotFound)
}

// Check returns the release when it exists.
func TestDelete_Check_Found(t *testing.T) {
	rel := &v1.Release{Name: "x-prod"}
	del := action.NewDelete(&mockDeleter{release: rel})
	result, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod"})
	require.NoError(t, err)
	assert.Equal(t, rel, result.Release)
	assert.False(t, result.NotFound)
}

// Execute deletes the release successfully.
func TestDelete_Execute_Success(t *testing.T) {
	del := action.NewDelete(&mockDeleter{})
	err := del.Execute(context.Background(), "x", "prod", false)
	require.NoError(t, err)
}

// Execute wraps deleter errors.
func TestDelete_Execute_Error(t *testing.T) {
	del := action.NewDelete(&mockDeleter{delErr: fmt.Errorf("helm delete failed")})
	err := del.Execute(context.Background(), "x", "prod", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete release")
}

// Execute passes wait=false to the deleter by default.
func TestDelete_Execute_WaitFalse(t *testing.T) {
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(context.Background(), "x", "prod", false)
	require.NoError(t, err)
	assert.False(t, m.gotWait, "expected wait=false to be forwarded to deleter")
}

// Execute passes wait=true to the deleter when requested.
func TestDelete_Execute_WaitTrue(t *testing.T) {
	m := &mockDeleter{}
	del := action.NewDelete(m)
	err := del.Execute(context.Background(), "x", "prod", true)
	require.NoError(t, err)
	assert.True(t, m.gotWait, "expected wait=true to be forwarded to deleter")
}

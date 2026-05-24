package action_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/deployah-dev/deployah/internal/action"
	"github.com/deployah-dev/deployah/internal/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "helm.sh/helm/v4/pkg/release/v1"
)

type mockDeleter struct {
	release *v1.Release
	getErr  error
	delErr  error
}

func (m *mockDeleter) GetRelease(_ context.Context, _, _ string) (*v1.Release, error) {
	return m.release, m.getErr
}

func (m *mockDeleter) DeleteRelease(_ context.Context, _, _ string) error {
	return m.delErr
}

func TestDelete_Check_NotFoundWithoutForce(t *testing.T) {
	del := action.NewDelete(&mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)})
	_, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod", Force: false})
	require.Error(t, err)
	assert.ErrorIs(t, err, helm.ErrReleaseNotFound)
}

func TestDelete_Check_NotFoundWithForce(t *testing.T) {
	del := action.NewDelete(&mockDeleter{getErr: fmt.Errorf("release 'x-prod': %w", helm.ErrReleaseNotFound)})
	result, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod", Force: true})
	require.NoError(t, err)
	assert.True(t, result.NotFound)
}

func TestDelete_Check_Found(t *testing.T) {
	rel := &v1.Release{Name: "x-prod"}
	del := action.NewDelete(&mockDeleter{release: rel})
	result, err := del.Check(context.Background(), action.DeleteParams{Project: "x", Environment: "prod"})
	require.NoError(t, err)
	assert.Equal(t, rel, result.Release)
	assert.False(t, result.NotFound)
}

func TestDelete_Execute_Success(t *testing.T) {
	del := action.NewDelete(&mockDeleter{})
	err := del.Execute(context.Background(), "x", "prod")
	require.NoError(t, err)
}

func TestDelete_Execute_Error(t *testing.T) {
	del := action.NewDelete(&mockDeleter{delErr: fmt.Errorf("helm delete failed")})
	err := del.Execute(context.Background(), "x", "prod")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete release")
}

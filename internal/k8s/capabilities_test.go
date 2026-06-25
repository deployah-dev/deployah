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

package k8s

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// fakeClientWithAPIs returns a fake clientset whose discovery layer reports
// the given group/version strings as available. Each string is of the form
// "group/version" (e.g. "autoscaling/v2") or "version" for core (e.g. "v1").
func fakeClientWithAPIs(groupVersions ...string) *fake.Clientset {
	cs := fake.NewClientset()
	resources := make([]*metav1.APIResourceList, 0, len(groupVersions))
	for _, gv := range groupVersions {
		resources = append(resources, &metav1.APIResourceList{
			GroupVersion: gv,
		})
	}
	cs.Resources = resources
	return cs
}

// TestCheckAPIRequirements_AllSatisfied verifies that CheckAPIRequirements
// returns nil when the cluster has all required API groups.
func TestCheckAPIRequirements_AllSatisfied(t *testing.T) {
	cs := fakeClientWithAPIs("autoscaling/v2", "networking.k8s.io/v1")

	reqs := []APIRequirement{
		{GroupVersions: []string{"autoscaling/v2"}, Reason: `required by component "web"`},
		{GroupVersions: []string{"networking.k8s.io/v1"}, Reason: `required by component "api"`},
	}

	err := CheckAPIRequirements(cs, reqs)
	assert.NoError(t, err)
}

// TestCheckAPIRequirements_OneMissing verifies that CheckAPIRequirements
// returns an error containing the reason when one API is missing.
func TestCheckAPIRequirements_OneMissing(t *testing.T) {
	cs := fakeClientWithAPIs("networking.k8s.io/v1")

	reqs := []APIRequirement{
		{GroupVersions: []string{"autoscaling/v2"}, Reason: `required by component "web" (autoscaling enabled)`},
	}

	err := CheckAPIRequirements(cs, reqs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "autoscaling/v2")
	assert.Contains(t, err.Error(), `required by component "web" (autoscaling enabled)`)
}

// TestCheckAPIRequirements_MultipleMissing verifies that all missing APIs
// are reported together in a single error.
func TestCheckAPIRequirements_MultipleMissing(t *testing.T) {
	cs := fakeClientWithAPIs("v1")

	reqs := []APIRequirement{
		{GroupVersions: []string{"autoscaling/v2"}, Reason: `required by component "web" (autoscaling enabled)`},
		{GroupVersions: []string{"networking.k8s.io/v1"}, Reason: `required by component "api" (ingress enabled)`},
	}

	err := CheckAPIRequirements(cs, reqs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "autoscaling/v2")
	assert.Contains(t, err.Error(), "networking.k8s.io/v1")
	assert.Contains(t, err.Error(), `required by component "web"`)
	assert.Contains(t, err.Error(), `required by component "api"`)
}

// TestCheckAPIRequirements_FallbackGroupVersion verifies that a requirement
// with multiple acceptable group/versions is satisfied when any one is present.
func TestCheckAPIRequirements_FallbackGroupVersion(t *testing.T) {
	cs := fakeClientWithAPIs("autoscaling/v2beta2") // v2 absent, v2beta2 present

	reqs := []APIRequirement{
		{
			GroupVersions: []string{"autoscaling/v2", "autoscaling/v2beta2"},
			Reason:        `required by component "web" (autoscaling enabled)`,
		},
	}

	err := CheckAPIRequirements(cs, reqs)
	assert.NoError(t, err, "fallback version should satisfy the requirement")
}

// TestCheckAPIRequirements_EmptyRequirements verifies that an empty
// requirements slice always returns nil.
func TestCheckAPIRequirements_EmptyRequirements(t *testing.T) {
	cs := fakeClientWithAPIs() // no APIs at all

	err := CheckAPIRequirements(cs, nil)
	assert.NoError(t, err)
}

// TestCheckAPIRequirements_ErrorFormatBulletList verifies that the error
// message uses the expected bullet-list format.
func TestCheckAPIRequirements_ErrorFormatBulletList(t *testing.T) {
	cs := fakeClientWithAPIs("v1")

	reqs := []APIRequirement{
		{GroupVersions: []string{"autoscaling/v2", "autoscaling/v2beta2"}, Reason: `required by component "web"`},
	}

	err := CheckAPIRequirements(cs, reqs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster does not support required APIs")
	assert.Contains(t, err.Error(), "autoscaling/v2 or autoscaling/v2beta2")
}

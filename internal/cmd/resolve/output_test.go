// Copyright 2025 The Deployah Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package resolve

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"nabat.dev/nabat"
	"nabat.dev/nabat/nabattest"

	"deployah.dev/deployah/internal/spec"
)

func TestHasDisplayableComponentResolution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rc   spec.ResolvedComponent
		want bool
	}{
		{name: "FQDN", rc: spec.ResolvedComponent{FQDN: "api.example.com"}, want: true},
		{name: "TLS", rc: spec.ResolvedComponent{TLSMode: spec.TLSModeCertManager}, want: true},
		{name: "Profiles", rc: spec.ResolvedComponent{Profiles: []string{"default"}}, want: true},
		{name: "MergedProfile", rc: spec.ResolvedComponent{MergedProfile: &spec.PlatformProfile{}}, want: true},
		{name: "StorageClass", rc: spec.ResolvedComponent{StorageClass: "fast-ssd"}, want: true},
		{name: "FileValues", rc: spec.ResolvedComponent{Runtime: spec.ResolvedRuntimeEnvironment{FileValues: map[string]string{"REGION": "eu"}}}, want: true},
		{name: "ExplicitValues", rc: spec.ResolvedComponent{Runtime: spec.ResolvedRuntimeEnvironment{ExplicitValues: map[string]string{"LOG_LEVEL": "debug"}}}, want: true},
		{name: "nothing", rc: spec.ResolvedComponent{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, hasDisplayableComponentResolution(tt.rc))
		})
	}
}

func TestOutputText_StorageClassOnlyComponent(t *testing.T) {
	t.Parallel()

	c, out := nabatContextWithOut(t)
	require.NoError(t, outputText(c, &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {StorageClass: "fast-ssd"},
		},
	}, &spec.ResolutionReport{Env: spec.NormalizeEnv("staging")}))

	got := out.String()
	assert.Contains(t, got, "api:")
	assert.Contains(t, got, "storageClass: fast-ssd")
}

func TestOutputText_EmptyComponentOmitted(t *testing.T) {
	t.Parallel()

	c, out := nabatContextWithOut(t)
	require.NoError(t, outputText(c, &spec.ResolvedSpec{
		Components: map[string]spec.ResolvedComponent{
			"api": {},
		},
	}, &spec.ResolutionReport{Env: spec.NormalizeEnv("staging")}))

	got := out.String()
	assert.NotContains(t, got, "api:")
	assert.NotContains(t, got, "storageClass")
	assert.NotContains(t, got, "Components:")
}

func nabatContextWithOut(t *testing.T) (*nabat.Context, *bytes.Buffer) {
	t.Helper()
	io, _, out, _ := nabattest.NewIO()
	app := nabat.MustNew("test", nabat.WithIO(io))
	return nabattest.Context(t, app), out
}

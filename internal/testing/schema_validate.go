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

package testing

import (
	"encoding/json"
	"slices"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

// unregisteredSchemeKinds lists kinds the embedded chart can emit that
// [validateAgainstScheme] cannot check against [scheme.Scheme]:
//
//   - ServiceMonitor, PodMonitor: Prometheus Operator CRDs, not core
//     Kubernetes types. No scenario renders them today.
//   - HorizontalPodAutoscaler: [helm.Client.RenderOffline] has no live
//     cluster, so Capabilities.KubeVersion falls back to Helm's pre-1.23
//     default, making the chart select the removed autoscaling/v2beta1 API
//     instead of autoscaling/v2. This affects `deployah plan --offline` for
//     any autoscaling component; the real fix belongs in the render
//     pipeline (a modern default KubeVersion), not here.
var unregisteredSchemeKinds = []string{"ServiceMonitor", "PodMonitor", "HorizontalPodAutoscaler"}

// schemeDecoder is a strict (unknown-field-rejecting) decoder against the
// client-go built-in scheme, shared across scenarios.
var schemeDecoder = serializer.NewCodecFactory(scheme.Scheme, serializer.EnableStrict).UniversalDeserializer()

// validateAgainstScheme decodes every manifest into its typed Kubernetes
// API struct (e.g. appsv1.Deployment), failing the test on an unknown
// field, wrong field type, or any other mismatch with the real Kubernetes
// OpenAPI schema that a subset/spec-only comparison would miss.
func validateAgainstScheme(t *testing.T, manifests []unstructured.Unstructured) {
	t.Helper()
	for _, manifest := range manifests {
		if slices.Contains(unregisteredSchemeKinds, manifest.GetKind()) {
			continue
		}

		data, err := json.Marshal(manifest.Object)
		if err != nil {
			t.Errorf("%s/%s: marshal for schema validation: %v", manifest.GetKind(), manifest.GetName(), err)
			continue
		}

		if _, _, decodeErr := schemeDecoder.Decode(data, nil, nil); decodeErr != nil {
			t.Errorf("%s/%s: failed Kubernetes schema validation: %v", manifest.GetKind(), manifest.GetName(), decodeErr)
		}
	}
}

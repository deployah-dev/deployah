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

package extras

import (
	"fmt"
	"strings"

	"deployah.dev/deployah/internal/spec"
)

// mergeIdentity applies Deployah identity labels/annotations and fills
// namespace for namespaced objects. Reserved deployah.dev/* keys always win;
// other common keys lose to the manifest. Names are never rewritten.
func mergeIdentity(o *Object, project, environment, source, releaseNamespace string, namespaced bool) error {
	if namespaced {
		ns := o.Obj.GetNamespace()
		if ns == "" {
			if releaseNamespace == "" {
				return fmt.Errorf("%s: namespaced object has empty metadata.namespace and no release namespace is set", o.Path)
			}
			o.Obj.SetNamespace(releaseNamespace)
		} else if releaseNamespace != "" && ns != releaseNamespace {
			return fmt.Errorf("%s: metadata.namespace %q differs from release namespace %q", o.Path, ns, releaseNamespace)
		}
	} else if o.Obj.GetNamespace() != "" {
		// Cluster-scoped objects must not carry a namespace.
		return fmt.Errorf("%s: cluster-scoped %s must not set metadata.namespace", o.Path, o.Obj.GetKind())
	}

	// Labels: reserved keys we own overwrite; other deployah.dev/* keys are
	// stripped so extras cannot impersonate reserved semantics. Never set
	// component on extras.
	labels := getMetaStringMap(o.Obj, "labels")
	if labels == nil {
		labels = map[string]string{}
	}
	labels[spec.LabelProject] = project
	keepEnv := source == spec.SourceManifests && environment != ""
	if keepEnv {
		labels[spec.LabelEnvironment] = environment
	}
	for k := range labels {
		if !strings.HasPrefix(k, spec.LabelPrefix+"/") {
			continue
		}
		if k == spec.LabelProject {
			continue
		}
		if keepEnv && k == spec.LabelEnvironment {
			continue
		}
		delete(labels, k)
	}
	o.Obj.SetLabels(labels)

	annotations := getMetaStringMap(o.Obj, "annotations")
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[spec.AnnotationSource] = source
	annotations[spec.AnnotationProject] = project
	// Strip any other reserved deployah.dev/* annotation keys the user set
	// that we do not own, so they cannot impersonate reserved semantics.
	for k := range annotations {
		if strings.HasPrefix(k, spec.LabelPrefix+"/") && k != spec.AnnotationSource && k != spec.AnnotationProject {
			delete(annotations, k)
		}
	}
	o.Obj.SetAnnotations(annotations)

	raw, err := o.MarshalYAML()
	if err != nil {
		return err
	}
	o.Raw = raw
	return nil
}

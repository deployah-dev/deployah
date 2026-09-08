package action

import (
	"deployah.dev/deployah/internal/helm"
	"deployah.dev/deployah/internal/spec"

	v1 "helm.sh/helm/v4/pkg/release/v1"
)

// matchingReleases drops nil entries. When environment is a wildcard
// instance such as review/pr-123, it keeps only that Helm release so a
// sibling review/pr-456 is not included. Logical names such as review
// keep every release that shared the environment label.
func matchingReleases(releases []*v1.Release, project, environment string) []*v1.Release {
	wantName := ""
	env := spec.NormalizeEnv(environment)
	if project != "" && environment != "" && env.Original != env.MapKey {
		wantName = helm.GenerateReleaseName(project, env.Original)
	}
	valid := make([]*v1.Release, 0, len(releases))
	for _, r := range releases {
		if r == nil {
			continue
		}
		if wantName != "" && r.Name != wantName {
			continue
		}
		valid = append(valid, r)
	}
	return valid
}

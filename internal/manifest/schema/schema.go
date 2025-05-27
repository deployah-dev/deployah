package schema

import (
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// FS is the embedded filesystem containing the schema files.
//
//go:embed **/*.json
var fs embed.FS

// SchemaType is the type of schema.
type SchemaType string

// String returns the string representation of the schema type.
func (s SchemaType) String() string {
	return string(s)
}

const (
	// SchemaTypeManifest is the type of schema for validating manifests.
	SchemaTypeManifest SchemaType = "manifest"
	// SchemaTypeEnvironments is the type of schema for validating environments.
	SchemaTypeEnvironments SchemaType = "environments"
)

var (
	// versionRegex is a regular expression that matches version strings in the format "v1-alpha.1", "v1-beta.2", etc.
	versionRegex = regexp.MustCompile(`^v(\d+)(?:-(alpha|beta|rc)\.(\d+))?$`)
	// preReleaseOrder is a map that defines the order of pre-release types.
	preReleaseOrder = map[string]int{"alpha": 0, "beta": 1, "rc": 2, "": 3}
)

// GetManifestSchema retrieves the JSON schema for validating manifests at a specific version.
// Version strings should follow the format "v1-alpha.1", "v1-beta.2", etc.
// The schema file must be named "manifest.json" within the version directory.
func GetManifestSchema(version string) ([]byte, error) {
	fileName := version + "/manifest.json"
	if _, err := fs.Open(fileName); err != nil {
		return nil, fmt.Errorf("manifest schema not found for version %s", version)
	}
	return fs.ReadFile(fileName)
}

// GetEnvironmentsSchema returns the environments schema for the given version.
// Version strings should follow the format "v1-alpha.1", "v1-beta.2", etc.
// The schema file must be named "environments.json" within the version directory.
func GetEnvironmentsSchema(version string) ([]byte, error) {
	fileName := version + "/environments.json"
	if _, err := fs.Open(fileName); err != nil {
		return nil, fmt.Errorf("environments schema not found for version %s", version)
	}
	return fs.ReadFile(fileName)
}

// GetManifestSchemas returns a map of version string to manifest schema []byte.
func GetManifestSchemas() (map[string][]byte, error) {
	files, err := fs.ReadDir(".")
	if err != nil {
		return nil, fmt.Errorf("failed to read schema directory: %w", err)
	}

	schemas := make(map[string][]byte)
	for _, file := range files {
		if !file.IsDir() {
			continue
		}
		version := file.Name()
		schema, err := GetManifestSchema(version)
		if err != nil {
			return nil, fmt.Errorf("failed to read schema for version %s: %w", version, err)
		}
		schemas[version] = schema
	}

	return schemas, nil
}

// getSortedVersions returns a sorted slice of version strings (ascending order)
func getSortedVersions() ([]string, error) {
	schemas, err := GetManifestSchemas()
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest schemas: %w", err)
	}

	versions := make([]string, 0, len(schemas))
	for v := range schemas {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareSchemaVersions(versions[i], versions[j]) < 0
	})
	return versions, nil
}

// GetLatestManifestSchema returns the latest manifest schema ([]byte) by version order.
func GetLatestManifestSchema() ([]byte, error) {
	versions, err := getSortedVersions()
	if err != nil {
		return nil, fmt.Errorf("failed to get sorted versions: %w", err)
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf("no manifest schemas found")
	}
	return GetManifestSchema(versions[len(versions)-1])
}

// GetLatestManifestVersion returns the latest manifest schema version (string).
func GetLatestManifestVersion() (string, error) {
	versions, err := getSortedVersions()
	if err != nil {
		return "", fmt.Errorf("failed to get sorted versions: %w", err)
	}
	if len(versions) == 0 {
		return "", fmt.Errorf("no manifest schemas found")
	}
	return versions[len(versions)-1], nil
}

func GetValidManifestVersions() ([]string, error) {
	versions, err := getSortedVersions()
	if err != nil {
		return nil, fmt.Errorf("failed to get sorted versions: %w", err)
	}
	return versions, nil
}

// compareSchemaVersions returns -1 if a < b, 0 if a == b, 1 if a > b
func compareSchemaVersions(a, b string) int {
	// parse splits a version string into major, pre-release type, and pre-release number
	parse := func(v string) (major int, pre string, preNum int, valid bool) {
		m := versionRegex.FindStringSubmatch(v)
		if m == nil {
			return 0, "", 0, false // fallback for invalid format
		}
		major, _ = strconv.Atoi(m[1])
		pre = m[2]
		if m[3] != "" {
			preNum, _ = strconv.Atoi(m[3])
		}
		return major, pre, preNum, true
	}

	majA, preA, numA, validA := parse(a)
	majB, preB, numB, validB := parse(b)

	// If either version is invalid, fall back to lexicographical order
	if !validA && !validB {
		return strings.Compare(a, b)
	} else if !validA {
		return 1 // invalid versions are considered greater (sorted last)
	} else if !validB {
		return -1
	}

	// Compare major versions first
	if majA != majB {
		return compareInts(majA, majB)
	}

	// Compare pre-release types (alpha < beta < rc < "")
	if preReleaseOrder[preA] != preReleaseOrder[preB] {
		return compareInts(preReleaseOrder[preA], preReleaseOrder[preB])
	}

	// Compare pre-release numbers
	return compareInts(numA, numB)
}

// compareInts returns -1 if a < b, 0 if a == b, 1 if a > b
func compareInts(a, b int) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}

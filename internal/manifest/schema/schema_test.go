// Package schema provides functions for managing manifest schemas.
package schema

import (
	"fmt"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

// SchemaTestSuite is a test suite for the schema package
type SchemaTestSuite struct {
	suite.Suite
}

// TestGetManifestSchema tests that the manifest schema is returned for the given version
func (s *SchemaTestSuite) TestGetManifestSchema() {
	schema, err := GetManifestSchema("v1-alpha.1")
	s.Require().NoError(err)
	s.Require().NotNil(schema)
}

func (s *SchemaTestSuite) TestGetManifestSchema_InvalidVersion() {
	schema, err := GetManifestSchema("invalid")
	s.Require().Error(err)
	s.Require().Nil(schema)
}

// TestGetEnvironmentsSchema tests that the environments schema is returned for the given version
func (s *SchemaTestSuite) TestGetEnvironmentsSchema() {
	schema, err := GetEnvironmentsSchema("v1-alpha.1")
	s.Require().NoError(err)
	s.Require().NotNil(schema)
}

func (s *SchemaTestSuite) TestGetEnvironmentsSchema_InvalidVersion() {
	schema, err := GetEnvironmentsSchema("invalid")
	s.Require().Error(err)
	s.Require().Nil(schema)
}

// TestCompareSchemaVersions tests the compareSchemaVersions function with table-driven subtests
func (s *SchemaTestSuite) TestCompareSchemaVersions() {
	cases := []struct {
		a, b   string
		expect int // -1 if a < b, 0 if a == b, 1 if a > b
	}{
		{"v1-alpha.1", "v1-beta.2", -1},
		{"v1-beta.2", "v1-beta.11", -1},
		{"v1-beta.11", "v1-rc.1", -1},
		{"v1-rc.1", "v1", -1},
		{"v1", "v1", 0},
		{"v2", "v1", 1},
		{"v1-beta.2", "v1-alpha.1", 1},
		{"v1-beta.2", "v1-beta.2", 0},
		{"v1-beta.2", "v1-beta.11", -1},
		{"v1-beta.11", "v1-beta.2", 1},
		{"v1-alpha.1", "v1-alpha.1", 0},
		{"v1-alpha.1", "v1-alpha.2", -1},
		{"v1-alpha.2", "v1-alpha.1", 1},
		{"v1", "v1-alpha.1", 1},
		{"v1", "v2", -1},
		{"v1-rc.1", "v1-beta.11", 1},
		{"v1-rc.1", "v1-rc.1", 0},
		{"v1-rc.2", "v1-rc.1", 1},
		{"v1-rc.1", "v1-rc.2", -1},
		{"v1", "invalid", -1},
		{"invalid", "v1", 1},
		{"invalid", "invalid2", -1},
	}
	for _, c := range cases {
		name := fmt.Sprintf("%s_vs_%s", c.a, c.b)
		s.T().Run(name, func(t *testing.T) {
			res := compareSchemaVersions(c.a, c.b)
			if c.expect < 0 {
				assert.Less(t, res, 0, "expected %s < %s", c.a, c.b)
			} else if c.expect > 0 {
				assert.Greater(t, res, 0, "expected %s > %s", c.a, c.b)
			} else {
				assert.Equal(t, 0, res, "expected %s == %s", c.a, c.b)
			}
		})
	}
}

// TestSortSchemaVersions tests sorting of schema version strings using compareSchemaVersions
func (s *SchemaTestSuite) TestSortSchemaVersions() {
	unsorted := []string{
		"v1-beta.2",
		"v1",
		"v1-alpha.1",
		"v1-beta.11",
		"v1-rc.1",
	}
	expected := []string{
		"v1-alpha.1",
		"v1-beta.2",
		"v1-beta.11",
		"v1-rc.1",
		"v1",
	}
	sorted := make([]string, len(unsorted))
	copy(sorted, unsorted)
	sort.Slice(sorted, func(i, j int) bool {
		return compareSchemaVersions(sorted[i], sorted[j]) < 0
	})
	assert.Equal(s.T(), expected, sorted, "schema versions should be sorted as expected")
}

// TestGetAllSchemas tests that the schemas are sorted according to compareSchemaVersions
func (s *SchemaTestSuite) TestGetAllSchemas() {
	schemas, err := GetManifestSchemas()
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), schemas)
	versions := make([]string, 0, len(schemas))
	for v := range schemas {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareSchemaVersions(versions[i], versions[j]) < 0
	})
	for i := 1; i < len(versions); i++ {
		cmp := compareSchemaVersions(versions[i-1], versions[i])
		assert.LessOrEqual(s.T(), cmp, 0, "schemas should be sorted: %s <= %s", versions[i-1], versions[i])
	}
}

// TestGetLatestSchema tests that the latest schema is the last entry in the sorted schemas list
func (s *SchemaTestSuite) TestGetLatestSchema() {
	schemas, err := GetManifestSchemas()
	assert.NoError(s.T(), err)
	if len(schemas) == 0 {
		s.T().Skip("no schemas available to test")
	}
	versions := make([]string, 0, len(schemas))
	for v := range schemas {
		versions = append(versions, v)
	}
	sort.Slice(versions, func(i, j int) bool {
		return compareSchemaVersions(versions[i], versions[j]) < 0
	})
	latest, err := GetLatestManifestSchema()
	assert.NoError(s.T(), err)
	// The latest schema should match the last entry in the sorted schemas list
	expected, err := GetManifestSchema(versions[len(versions)-1])
	assert.NoError(s.T(), err)
	assert.Equal(s.T(), expected, latest)
}

// TestGetLatestVersion tests that the latest version is returned
func (s *SchemaTestSuite) TestGetLatestManifestVersion() {
	latest, err := GetLatestManifestVersion()
	assert.NoError(s.T(), err)
	assert.NotEmpty(s.T(), latest)
}

// TestSchemaTestSuite runs the schema test suite
func TestSchemaTestSuite(t *testing.T) {
	suite.Run(t, new(SchemaTestSuite))
}

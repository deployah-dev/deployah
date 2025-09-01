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

package helm

// Chart and Template Constants
const (
	// ChartNameTemplate is the template for generated chart names
	ChartNameTemplate = "%s-%s"

	// ReleaseNameTemplate is the template for Helm release names
	ReleaseNameTemplate = "%s-%s"

	// TempChartPrefix is the prefix for temporary chart directories
	TempChartPrefix = "deployah-chart-"

	// ChartYamlTemplate is the template file name for Chart.yaml
	ChartYamlTemplate = "Chart.yaml.gotmpl"

	// ValuesYamlFile is the name of the values file
	ValuesYamlFile = "values.yaml"
)

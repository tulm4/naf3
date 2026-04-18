// Copyright 2024 APRESIA Systems LTD.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package openapi_test

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/ShouheiNishi/openapi5g/internal/generator/openapi"
)

func TestTypes(t *testing.T) {
	dir := "../../../specs"
	dirEntries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, dirEntry := range dirEntries {
		name := dirEntry.Name()
		if dirEntry.Type().IsRegular() && strings.HasSuffix(name, ".yaml") {
			path := filepath.Join(dir, name)
			t.Run(name, func(t *testing.T) {
				t.Parallel()

				fYAML, err := os.Open(path)
				require.NoError(t, err)
				yamlBufOrig, err := io.ReadAll(fYAML)
				require.NoError(t, err)

				var doc openapi.Document
				err = yaml.Unmarshal(yamlBufOrig, &doc)
				assert.NoError(t, err)
				yamlBuf, err := yaml.Marshal(doc)
				assert.NoError(t, err)
				assert.YAMLEq(t, string(yamlBufOrig), string(yamlBuf))
			})
		}
	}
}

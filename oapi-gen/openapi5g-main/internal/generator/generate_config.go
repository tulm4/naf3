// Copyright 2023-2024 APRESIA Systems LTD.
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

package generator

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

func (s *GeneratorState) GenerateConfig(spec string) error {
	e := pkgList[spec]

	_, base := filepath.Split(e.path)

	top := "specs"
	for i := 0; i <= strings.Count(e.path, "/"); i++ {
		top = filepath.Join("..", top)
	}

	modTop := "modSpecs"
	for i := 0; i <= strings.Count(e.path, "/"); i++ {
		modTop = filepath.Join("..", modTop)
	}

	doc := s.Specs[spec]
	if doc == nil {
		return fmt.Errorf("spec %s is not exist", spec)
	}

	if f, err := s.CreateFile(e.path, "config.yaml"); err != nil {
		return fmt.Errorf("CreateFile(%s, \"config.yaml\"): %w", e.path, err)
	} else {
		fmt.Fprintf(f, "# This is generated file, DO NOT EDIT.\n")
		fmt.Fprintf(f, "package: %s\n", base)
		fmt.Fprintf(f, "generate:\n")
		fmt.Fprintf(f, "  models: true\n")
		if len(doc.Paths) != 0 {
			fmt.Fprintf(f, "  client: true\n")
			fmt.Fprintf(f, "  gin-server: true\n")
		}
		if len(doc.Paths) != 0 || (doc.Components != nil && len(doc.Components.Responses) != 0) {
			fmt.Fprintf(f, "  strict-server: true\n")
		}
		fmt.Fprintf(f, "output-options:\n")
		fmt.Fprintf(f, "  skip-prune: true\n")
		if deps := s.DepsForImport[spec]; len(deps) != 0 {
			fmt.Fprintf(f, "import-mapping:\n")
			for _, d := range deps {
				fmt.Fprintf(f, "  %s: %s/%s\n", d, modBase, pkgList[d].path)
			}
		}
		fmt.Fprintf(f, "output: %s.go\n", base)

		if err := f.Close(); err != nil {
			return fmt.Errorf("Close(): %w", err)
		}
	}

	if n, err := s.CreateFileName(e.path, "generate.go"); err != nil {
		return fmt.Errorf("CreateFileName(%s, \"generate.go\"): %w", e.path, err)
	} else {
		f := writer.NewOutputFile(n, base, generatorName, writer.ImportSpecs{})
		fmt.Fprintf(f, "//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=config.yaml %s\n", filepath.Join(modTop, spec))
		if err := f.Close(); err != nil {
			return fmt.Errorf("Close(): %w", err)
		}
	}

	if _, err := s.CreateFileName(e.path, base+".go"); err != nil {
		return fmt.Errorf("CreateFileName(%s, %s): %w", e.path, base+".go", err)
	}

	return nil
}

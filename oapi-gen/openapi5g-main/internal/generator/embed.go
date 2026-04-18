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
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

func (s *GeneratorState) GenerateEmbed() error {
	for d := range s.Specs {
		base := strings.TrimSuffix(d, ".yaml")

		if name, err := s.CreateFileName("internal/embed", base, "embed.go"); err != nil {
			return fmt.Errorf("CreateFileName: %w", err)
		} else {
			out := writer.NewOutputFile(name, base, generatorName, writer.ImportSpecs{
				{PackageName: "_", ImportPath: "embed"},
			})
			fmt.Fprintf(out, "//go:embed %s\n", d)
			fmt.Fprintln(out, "var SpecYaml []byte")
			if err := out.Close(); err != nil {
				return err
			}
		}

		if fIn, err := os.Open(filepath.Join(s.RootDir, "specs", d)); err != nil {
			return err
		} else {
			defer fIn.Close()
			if fOut, err := s.CreateFile("internal/embed", base, d); err != nil {
				return err
			} else {
				defer fOut.Close()
				if _, err := io.Copy(fOut, fIn); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

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

package generator

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

func (s *GeneratorState) GeneratePkgMap() error {
	name, err := s.CreateFileName("utils/pkgmap/map.gen.go")
	if err != nil {
		return fmt.Errorf("CreateFileName: %w", err)
	}
	imports := writer.ImportSpecs{
		{ImportPath: "github.com/ShouheiNishi/openapi5g/utils/loader"},
	}
	for _, p := range pkgList {
		imports = append(imports, writer.ImportSpec{
			PackageName: strings.ReplaceAll(p.path, "/", "_"),
			ImportPath:  modBase + "/" + p.path,
		})
	}
	out := writer.NewOutputFile(name, "pkgmap", generatorName, imports)

	s2p := make(map[string]string)
	p2s := make(map[string]string)
	var specs []string
	var pkgs []string

	for s, p := range pkgList {
		pkg := modBase + "/" + p.path
		s2p[s] = pkg
		p2s[pkg] = s
		specs = append(specs, s)
		pkgs = append(pkgs, pkg)
	}
	sort.Strings(specs)
	sort.Strings(pkgs)

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "var s2p = map[string]string{")
	for _, s := range specs {
		fmt.Fprintf(out, "\"%s\": \"%s\",\n", s, s2p[s])
	}
	fmt.Fprintln(out, "}")

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "var p2s = map[string]string{")
	for _, p := range pkgs {
		fmt.Fprintf(out, "\"%s\": \"%s\",\n", p, p2s[p])
	}
	fmt.Fprintln(out, "}")

	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "var p2l = map[string]loader.SpecLoader{")
	for _, p := range pkgs {
		fmt.Fprintf(out, "\"%s\": %s.GetLoader(),\n", p, strings.ReplaceAll(p[len(modBase)+1:], "/", "_"))
	}
	fmt.Fprintln(out, "}")

	if err := out.Close(); err != nil {
		return err
	}

	return nil
}

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
	"path"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/ShouheiNishi/openapi5g/internal/generator/openapi"
)

type GeneratorState struct {
	RootDir       string
	OutFiles      map[string]struct{}
	Specs         map[string]*openapi.Document
	DepsBase      map[string]map[string]struct{}
	DepsForImport map[string][]string
	DepsForLoader map[string][]string
	CurSpec       string
}

func Generate(rootDir string) error {
	s := GeneratorState{
		OutFiles:      make(map[string]struct{}),
		Specs:         make(map[string]*openapi.Document),
		DepsBase:      make(map[string]map[string]struct{}),
		DepsForImport: make(map[string][]string),
	}
	if abs, err := filepath.Abs(rootDir); err != nil {
		return fmt.Errorf("filepath.Abs: %w", err)
	} else {
		s.RootDir = abs
	}

	if err := s.RewriteYamlAndGenerateConfig(); err != nil {
		return fmt.Errorf("RewriteYamlAndGenerateConfig: %w", err)
	}

	if err := s.GenerateEmbed(); err != nil {
		return fmt.Errorf("GenerateEmbed: %w", err)
	}

	if err := s.GenerateLoaderTest(); err != nil {
		return fmt.Errorf("GenerateLoaderTest: %w", err)
	}

	if err := s.GeneratePkgMap(); err != nil {
		return fmt.Errorf("GeneratePkgMap: %w", err)
	}

	if err := s.RemoveOldFiles(); err != nil {
		return fmt.Errorf("RemoveOldFiles: %w", err)
	}

	return nil
}

func (s *GeneratorState) RewriteYamlAndGenerateConfig() error {
	for spec := range pkgList {
		if s.Specs[spec] == nil {
			if err := s.LoadSpec(spec); err != nil {
				return fmt.Errorf("LoadSpec(%s): %w", spec, err)
			}
		}
	}

	if err := s.MakeDepsForLoader(); err != nil {
		return fmt.Errorf("MakeDepsForLoader(): %w", err)
	}

	if err := s.RewriteSpecs(); err != nil {
		return fmt.Errorf("RewriteSpecs(): %w", err)
	}

	for spec := range pkgList {
		if err := s.WriteSpec(spec); err != nil {
			return fmt.Errorf("WriteSpec(%s): %w", spec, err)
		}

		if err := s.GenerateConfig(spec); err != nil {
			return fmt.Errorf("GenerateConfig(%s): %w", spec, err)
		}

		if err := s.GenerateLoader(spec); err != nil {
			return fmt.Errorf("GenerateLoader(: %w", err)
		}
	}
	return nil
}

func (s *GeneratorState) CreateFileName(paths ...string) (string, error) {
	newPath := path.Join(append([]string{s.RootDir}, paths...)...)
	newDir := path.Dir(newPath)
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		return "", fmt.Errorf("MkdirAll(%s): %w", newDir, err)
	}
	if _, exist := s.OutFiles[newPath]; exist {
		return "", fmt.Errorf("file %s is already created", newPath)
	}
	s.OutFiles[newPath] = struct{}{}
	return newPath, nil
}

func (s *GeneratorState) CreateFile(paths ...string) (io.WriteCloser, error) {
	if newPath, err := s.CreateFileName(paths...); err != nil {
		return nil, fmt.Errorf("CreateFile(%v): %w", paths, err)
	} else if f, err := os.Create(newPath); err != nil {
		return nil, fmt.Errorf("Create(%s): %w", newPath, err)
	} else {
		return f, nil
	}
}

func (s *GeneratorState) LoadSpec(spec string) error {
	if f, err := os.Open(filepath.Join(s.RootDir, "specs", spec)); err != nil {
		return fmt.Errorf("os.Open(%s): %w", spec, err)
	} else {
		defer f.Close()

		if buf, err := io.ReadAll(f); err != nil {
			return fmt.Errorf("io.ReadAll(%s): %w", spec, err)
		} else {
			var doc openapi.Document
			if err = yaml.Unmarshal(buf, &doc); err != nil {
				return fmt.Errorf("yaml.Unmarshal(%s): %w", spec, err)
			} else {
				s.Specs[spec] = &doc
				deps := make(map[string]struct{})
				if err := setupRefs(&doc, spec, deps); err != nil {
					return fmt.Errorf("setupRefs(%s): %w", spec, err)
				} else {
					s.DepsBase[spec] = deps
					return resolveRefs(&doc, s)
				}
			}
		}
	}
}

func (s *GeneratorState) WriteSpec(spec string) error {
	if doc := s.Specs[spec]; doc == nil {
		return fmt.Errorf("spec %s is not exist", spec)
	} else if err := postRefs(doc, &s.CurSpec); err != nil {
		return fmt.Errorf("postRefs(%s): %w", spec, err)
	} else if f, err := s.CreateFile("modSpecs", spec); err != nil {
		return fmt.Errorf("CreateFile(\"modSpecs\", \"%s\"): %w", spec, err)
	} else {
		defer f.Close()
		fmt.Fprintf(f, "# This is generated file.\n\n")
		s.CurSpec = spec
		if err := yaml.NewEncoder(f).Encode(doc); err != nil {
			return fmt.Errorf("encode for %s: %w", spec, err)
		} else {
			return nil
		}
	}
}

func ResolveRef[T any](s *GeneratorState, ref openapi.Reference) (*T, error) {
	if ref.Path == "" || ref.Pointer == "" {
		return nil, fmt.Errorf("invalid ref %s", ref)
	}
	spec := ref.Path
	if s.Specs[spec] == nil {
		if err := s.LoadSpec(spec); err != nil {
			return nil, fmt.Errorf("LoadSpec(%s): %w", spec, err)
		} else if s.Specs[spec] == nil {
			return nil, fmt.Errorf("LoadSpec(%s): not loaded", spec)
		}
	}

	if v, err := s.Specs[spec].GetFromJsonPointer(ref.Pointer); err != nil {
		return nil, fmt.Errorf("GetFromJsonPointer(%s - %s): %w", spec, ref.Pointer, err)
	} else if r, ok := v.(*T); ok {
		return r, nil
	} else if r, ok := v.(*openapi.Ref[T]); ok {
		if r == nil {
			return nil, fmt.Errorf("nil Ref pointer")
		} else if !r.HasRef() {
			if r.Value == nil {
				return nil, fmt.Errorf("nil Value pointer")
			} else {
				return r.Value, nil
			}
		} else {
			if r2, err := ResolveRef[T](s, r.Ref); err != nil {
				return nil, fmt.Errorf("ResolvedRef(%s): %w", r.Ref, err)
			} else {
				return r2, nil
			}
		}
	} else {
		return nil, fmt.Errorf("invalid resolved type %T", v)
	}
}

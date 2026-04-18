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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func (s *GeneratorState) RemoveOldFiles() error {
	return filepath.WalkDir(s.RootDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if path == filepath.Join(s.RootDir, ".git") {
			if d.IsDir() {
				return filepath.SkipDir
			} else {
				return nil
			}
		}

		if path == filepath.Join(s.RootDir, ".github") {
			return filepath.SkipDir
		}

		if path == filepath.Join(s.RootDir, "specs") {
			return filepath.SkipDir
		}

		if path == filepath.Join(s.RootDir, "utils") {
			return filepath.SkipDir
		}

		if path == filepath.Join(s.RootDir, "internal/generator") {
			return filepath.SkipDir
		}

		if strings.HasPrefix(path, filepath.Join(s.RootDir, "internal/test")+"/") && !strings.HasSuffix(path, "_gen_test.go") && !strings.HasSuffix(path, "_gen.go") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "internal/generate.go") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "models/problemdetails.go") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "models/problemdetails_test.go") {
			return nil
		}

		if path == filepath.Join(s.RootDir, ".gitmodules") {
			return nil
		}

		if path == filepath.Join(s.RootDir, ".gitignore") {
			return nil
		}

		if path == filepath.Join(s.RootDir, ".golangci.yml") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "LICENSE") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "NOTICE") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "README.md") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "go.mod") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "go.sum") {
			return nil
		}

		if path == filepath.Join(s.RootDir, "internal/tools.go") {
			return nil
		}

		if _, exist := s.OutFiles[path]; exist {
			return nil
		}

		if d.Type().IsRegular() {
			os.Remove(path)
		}

		return nil
	})
}

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

package main

import (
	"fmt"
	"go/types"
	"io"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

func main() {
	f := writer.NewOutputFile("pointer_gen.go",
		"openapi",
		"github.com/ShouheiNishi/openapi5g/internal/generator/openapi/pointer/generator.go",
		nil,
	)

	if pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedDeps | packages.NeedImports,
	}, "."); err != nil {
		panic(err)
	} else {
		for _, pkg := range pkgs {
			if pkg.PkgPath == "github.com/ShouheiNishi/openapi5g/internal/generator/openapi" {
				if err := generateJsonPointer(f, pkg); err != nil {
					panic(err)
				}
			}
		}
	}

	if err := f.Close(); err != nil {
		panic(err)
	}
}

func generateJsonPointer(f io.Writer, pkg *packages.Package) error {
	scope := pkg.Types.Scope()
	names := scope.Names()
	sort.Strings(names)

	for _, name := range names {
		if name == "Ref" || name == "AdditionalProperties" {
			continue
		}
		obj := scope.Lookup(name)
		if tn, _ := obj.(*types.TypeName); tn != nil /*&& tn.Exported()*/ {
			if nt, _ := tn.Type().(*types.Named); nt != nil && nt.Obj() == tn {
				if st, _ := nt.Underlying().(*types.Struct); st != nil {
					generateForSturct(f, name, st)
				}
			}
		}
	}

	return nil
}

func typeName(t types.Type) string {
	return types.TypeString(t,
		func(p *types.Package) string {
			return p.Name()
		})
}

func generateForSturct(f io.Writer, name string, s *types.Struct) {
	fieldsMap := make(map[string]*types.Var)
	for i := 0; i < s.NumFields(); i++ {
		f := s.Field(i)
		n := f.Name()
		if t := reflect.StructTag(s.Tag(i)).Get("yaml"); t != "" {
			if n = strings.Split(t, ",")[0]; n == "-" {
				continue
			}
		}
		fieldsMap[n] = f
	}
	fieldsKeys := make([]string, 0, len(fieldsMap))
	for n := range fieldsMap {
		fieldsKeys = append(fieldsKeys, n)
	}
	sort.Strings(fieldsKeys)

	fmt.Fprintf(f, "func (s *%s) GetFromJsonPointerSub(p jsonPointer) (any, error) {\n", name)
	fmt.Fprintf(f, "switch p[0] {\n")
	for _, n := range fieldsKeys {
		fmt.Fprintf(f, "case \"%s\":\n", n)
		field := fieldsMap[n]
		v := "s." + field.Name()
		depth := 0
		fieldType := field.Type()
		traverseType := fieldType
	loopTraverseType:
		for {
			ptrLevel := 0
		loopExtractType:
			for {
				switch t := traverseType.(type) {
				case *types.Pointer:
					traverseType = t.Elem()
					ptrLevel++
				case *types.Named:
					tUnderlying := t.Underlying()
					if s, _ := tUnderlying.(*types.Struct); s != nil {
						break loopExtractType
					}
					traverseType = tUnderlying
				default:
					break loopExtractType
				}
			}

			depth++
			if depth != 1 {
				fmt.Fprintf(f, "} else ")
			}
			fmt.Fprintf(f, "if len(p) == %d {\n", depth)
			fmt.Fprintf(f, "return %s, nil\n", v)

			setupPtr := func(receiver bool) {
				for i := 0; i < ptrLevel; i++ {
					fmt.Fprintf(f, "} else if %s == nil {\n", v)
					fmt.Fprintf(f, "return nil, errors.New(\"cannot traverse nil pointer\")\n")
					if !receiver || i < ptrLevel-1 {
						v = "*" + v
					}
				}
			}

			switch t := traverseType.(type) {
			case *types.Slice:
				setupPtr(false)
				fmt.Fprintf(f, "} else if i%d, err := strconv.Atoi(p[%d]) ; err != nil {\n", depth, depth)
				fmt.Fprintf(f, "return nil, fmt.Errorf(\"invalid index %%s\", p[%d])", depth)
				fmt.Fprintf(f, "} else if i%d < 0 || i%d >= len(%s) {\n", depth, depth, v)
				fmt.Fprintf(f, "return nil, fmt.Errorf(\"invalid index: length = %%d, index = %%d\", len(%s), i%d)", v, depth)
				traverseType = t.Elem()
				v = fmt.Sprintf("%s[i%d]", v, depth)
			case *types.Map:
				setupPtr(false)
				fmt.Fprintf(f, "} else if _, exist := %s[p[%d]] ; !exist {\n", v, depth)
				fmt.Fprintf(f, "return nil, fmt.Errorf(\"key %%s is not exist\", p[%d])", depth)
				traverseType = t.Elem()
				v = fmt.Sprintf("%s[p[%d]]", v, depth)
			case *types.Named:
				if t.Obj().Name() == "Node" {
					fmt.Fprintf(f, "} else {\n")
					fmt.Fprintf(f, "return nil, errors.New(\"cannot traverse type %s\")\n", typeName(fieldType))
					break loopTraverseType
				}
				setupPtr(true)
				fmt.Fprintf(f, "} else {\n")
				fmt.Fprintf(f, "return %s.GetFromJsonPointerSub(p[%d:])\n", v, depth)
				break loopTraverseType
			default:
				fmt.Fprintf(f, "} else {\n")
				fmt.Fprintf(f, "return nil, errors.New(\"cannot traverse type %s\")\n", typeName(fieldType))
				break loopTraverseType
			}
		}
		fmt.Fprintf(f, "}\n")
	}
	fmt.Fprintf(f, "default:\n")
	fmt.Fprintf(f, "return nil, fmt.Errorf(\"unknown entry %%s\", p[0])\n")
	fmt.Fprintf(f, "}\n")
	fmt.Fprintf(f, "}\n")
	fmt.Fprintf(f, "\n")
}

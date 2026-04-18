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

package main

import (
	"go/types"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

func main() {
	if err := LoadPackage("../.."); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "setup_refs_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "setupRefs",
		ExtraRootArgs: ", curFile string, deps map[string]struct{}",
		ExtraInit:     "s.curFile = curFile\ns.deps = deps\n",

		StateType:  "setupRefsType",
		ExtraState: "curFile string\ndeps map[string]struct{}\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Ref" {
				return `if v.HasRef() {
					if v.Ref.Path == "" {
						v.Ref.Path = s.curFile
					} else {
						s.deps[v.Ref.Path] = struct{}{}
					}
					return nil
				}
`
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "resolve_refs_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "resolveRefs",
		ExtraRootArgs: ",gs *GeneratorState",
		ExtraInit:     "s.generatorState = gs\n",

		StateType:  "resolveRefsType",
		ExtraState: "generatorState *GeneratorState\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Ref" {
				return `if v.HasRef() {
					if vRes, err := ResolveRef[` + typeName(t.TypeArgs().At(0).(*types.Named)) + `](s.generatorState, v.Ref); err != nil {
						return fmt.Errorf("ResolveRef(%s): %w", v.Ref, err)
					} else {
						v.Value = vRes
						return nil
					}
				}
`
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "post_refs_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "postRefs",
		ExtraRootArgs: ", curFile *string",
		ExtraInit:     "s.curFile = curFile\n",

		StateType:  "postRefsType",
		ExtraState: "curFile *string\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Ref" {
				return `v.CurFile = s.curFile
				if v.HasRef() {
					return nil
				}
`
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "rewrite_specs_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "walkRewriteSpecs",
		ExtraRootArgs: ", refs map[string]struct{}",
		ExtraInit:     "s.refs = refs\n",

		StateType:  "walkRewriteSpecsType",
		ExtraState: "refs map[string]struct{}\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Ref" {
				if t.TypeArgs().At(0).(*types.Named).Obj().Name() != "PathItemBase" {
					return `if v.HasRef() {
								s.refs[v.Ref.Path] = struct{}{}
								return nil
							}
`
				}
			}
			return ""
		},

		WalkPostHook: func(t *types.Named) string {
			if t.Obj().Name() == "Schema" {
				return "if err := fixSkipOptionalPointer(v) ; err != nil{return err}\n" +
					"\nif err := fixIntegerFormat(v) ; err != nil{return err}\n" +
					"\nif err := fixAnyOfEnum(v) ; err != nil{return err}\n" +
					"\nif err := fixAnyOfString(v) ; err != nil{return err}\n" +
					"\nif err := fixNullable(v) ; err != nil{return err}\n" +
					"\nif err := fixImplicitArray(v) ; err != nil{return err}\n" +
					"\nif err := fixEliminateCheckerUnion(v) ; err != nil{return err}\n" +
					"\nif err := fixAdditionalProperties(v) ; err != nil{return err}\n"
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "schema_ref_enumeration_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "walkSchemaRefEnumeration",
		ExtraRootArgs: ", schemasRefs map[openapi.Reference]struct{}",
		ExtraInit:     "s.schemasRefs = schemasRefs\n",

		StateType:  "walkSchemaRefEnumerationType",
		ExtraState: "schemasRefs map[openapi.Reference]struct{}\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Components" {
				return "return nil\n"
			} else if t.Obj().Name() == "Ref" && t.TypeArgs().At(0).(*types.Named).Obj().Name() == "Schema" {
				return `if v.HasRef() {
							s.schemasRefs[v.Ref] = struct{}{}
						}
`
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}

	if err := OpenApi3WalkerGenerate(&OpenApi3WalkerGeneratorConfig{
		FileName:      "schema_ref_remap_gen.go",
		PkgName:       "generator",
		GeneratorName: "github.com/ShouheiNishi/openapi5g/internal/generator/sub",
		Imports: writer.ImportSpecs{
			{ImportPath: "github.com/ShouheiNishi/openapi5g/internal/generator/openapi"},
			{ImportPath: "gopkg.in/yaml.v3"},
		},

		RootFuncName:  "walkSchemaRefRemap",
		ExtraRootArgs: ", refMap map[openapi.Reference]openapi.Reference",
		ExtraInit:     "s.refMap = refMap\n",

		StateType:  "walkSchemaRefRemapType",
		ExtraState: "refMap map[openapi.Reference]openapi.Reference\n",

		WalkPreHook: func(t *types.Named) string {
			if t.Obj().Name() == "Ref" && t.TypeArgs().At(0).(*types.Named).Obj().Name() == "Schema" {
				return `if v.HasRef() {
							if newRef, exist := s.refMap[v.Ref]; exist {
								v.Ref = newRef
							}
						}
`
			}
			return ""
		},
	}); err != nil {
		panic(err)
	}
}

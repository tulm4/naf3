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
	"errors"
	"fmt"
	"go/types"
	"io"
	"path"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"

	"github.com/ShouheiNishi/openapi5g/internal/generator/writer"
)

type OpenApi3WalkerGeneratorConfig struct {
	FileName      string
	PkgName       string
	GeneratorName string
	Imports       writer.ImportSpecs

	RootFuncName      string
	ExtraRootArgs     string
	ExtraInit         string
	ExtraWalkArgsInit string

	StateType  string
	ExtraState string

	ExtraWalkArgs     string
	ExtraWalkArgsCall string
	WalkPreHook       func(t *types.Named) string
	WalkPostHook      func(t *types.Named) string
}

type openApi3WalkerGeneratorState struct {
	*OpenApi3WalkerGeneratorConfig
	out   io.WriteCloser
	funcs map[string]string
}

const generatedFunctionPrefix = "walk"

var anyType = types.Universe.Lookup("any").Type()

var documentType *types.Named

func funcName(t *types.Named) string {
	n := t.Obj().Name()
	if tl := t.TypeArgs(); tl != nil {
		n = tl.At(0).(*types.Named).Obj().Name() + n
	}
	return generatedFunctionPrefix + n
}

func typeName(t *types.Named) string {
	return types.TypeString(t,
		func(p *types.Package) string {
			return p.Name()
		})
}

func (s *openApi3WalkerGeneratorState) generateValueOp(n string, t types.Type, depth int, id int) string {
	switch t := t.(type) {
	case *types.Named:
		u := t.Underlying()
		if st, _ := u.(*types.Struct); st == nil {
			return s.generateValueOp(n, u, depth, id)
		}
		if !strings.HasSuffix(t.Obj().Pkg().Path(), "/openapi") {
			return ""
		}
		s.generateFuncs(t)
		if s.funcs[funcName(t)] == "*" {
			return ""
		}
		n2 := "&" + n
		if strings.HasPrefix(n, "*") {
			n2 = strings.TrimPrefix(n, "*")
		}
		return fmt.Sprintf("if err := s.%s(%s %s) ; err != nil {\nreturn err\n}", funcName(t), n2, s.ExtraWalkArgsCall)
	case *types.TypeParam:
	case *types.Basic:
		return ""
	case *types.Interface:
		return fmt.Sprintf("if %s != nil {panic(\"Non nil interface\")}", n)
	case *types.Pointer:
		str := s.generateValueOp("*"+n, t.Elem(), depth+1, id)
		if str == "" {
			return ""
		}
		return fmt.Sprintf("if %s != nil {\n%s\n}", n, str)
	case *types.Slice:
		n2 := n
		if strings.HasPrefix(n, "*") {
			n2 = "(" + n + ")"
		}
		str := s.generateValueOp(fmt.Sprintf("%s[i%d]", n2, depth), t.Elem(), depth+1, id)
		if str == "" {
			return ""
		}
		return fmt.Sprintf("for i%d := range %s {\n%s\n}", depth, n, str)
	case *types.Map:
		n2 := n
		if strings.HasPrefix(n, "*") {
			n2 = "(" + n + ")"
		}
		str := s.generateValueOp(fmt.Sprintf("%s[k%d]", n2, depth), t.Elem(), depth+1, id)
		if str == "" {
			return ""
		}
		if t.Key() == types.Typ[types.String] {
			return fmt.Sprintf("k%ds%d := make([]string, 0, len(%s))\nfor k%d := range %s {\nk%ds%d = append(k%ds%d, k%d)\n}\n"+
				"sort.Strings(k%ds%d)\nfor _, k%d := range k%ds%d {\n%s\n}",
				depth, id, n, depth, n, depth, id, depth, id, depth,
				depth, id, depth, depth, id, str)
		} else {
			return fmt.Sprintf("for k%d := range %s {\n%s\n}", depth, n, str)
		}
	default:
		panic(fmt.Sprintf("%s is unsupported type %T", t, t))
	}
	return ""
}

func (s *openApi3WalkerGeneratorState) generateFuncs(t *types.Named) {
	st, _ := t.Underlying().(*types.Struct)
	if st == nil {
		panic(fmt.Sprintf("%s is not struct", t))
	}

	fname := funcName(t)

	if _, exist := s.funcs[fname]; exist {
		return
	}
	s.funcs[fname] = ""

	str := fmt.Sprintf("func (s *%s) %s(v *%s %s) error {\n", s.StateType, fname, typeName(t), s.ExtraWalkArgs)
	str += "if _, exist := s.visited[v] ; exist {\nreturn nil\n}\ns.visited[v] = struct{}{}\n"

	var elems []string
	elemIndex := make(map[string]int)
fieldLoop:
	for i := 0; i < st.NumFields(); i++ {
		f := st.Field(i)
		fName := f.Name()
		switch fName {
		case "Maximum":
			if t.Obj().Name() == "Schema" && f.Type() == anyType {
				continue fieldLoop
			}
		case "Minimum":
			if t.Obj().Name() == "Schema" && f.Type() == anyType {
				continue fieldLoop
			}
		}
		elems = append(elems, fName)
		elemIndex[fName] = i
	}

	sort.Strings(elems)
	empty := true

	if s.WalkPreHook != nil {
		if hs := s.WalkPreHook(t); hs != "" {
			str += "\n" + hs
			empty = false
		}
	}

	for i, n := range elems {
		f := st.Field(elemIndex[n])
		if s2 := s.generateValueOp("v."+n, f.Type(), 0, i); s2 != "" {
			str += "\n" + s2 + "\n"
			empty = false
		}
	}

	if s.WalkPostHook != nil {
		if hs := s.WalkPostHook(t); hs != "" {
			str += "\n" + hs
			empty = false
		}
	}

	str += "\nreturn nil\n}\n"

	if empty {
		s.funcs[fname] = "*"
		return
	}

	s.funcs[fname] = str
}

func OpenApi3WalkerGenerate(c *OpenApi3WalkerGeneratorConfig) error {
	s := openApi3WalkerGeneratorState{
		OpenApi3WalkerGeneratorConfig: c,
		out:                           writer.NewOutputFile(c.FileName, c.PkgName, c.GeneratorName, c.Imports),
		funcs:                         make(map[string]string),
	}

	fmt.Fprintf(s.out, "type %s struct{", c.StateType)
	fmt.Fprintln(s.out, "visited map[interface{}]struct{}")
	fmt.Fprintln(s.out, "doc *openapi.Document")
	fmt.Fprint(s.out, s.ExtraState)
	fmt.Fprintln(s.out, "}")
	fmt.Fprintln(s.out, "")

	fmt.Fprintf(s.out, "func %s(d *openapi.Document %s) error {\n", c.RootFuncName, c.ExtraRootArgs)
	fmt.Fprintf(s.out, "s := &%s {\n", c.StateType)
	fmt.Fprintln(s.out, "visited: make(map[interface{}]struct{}),")
	fmt.Fprintln(s.out, "doc: d,")
	fmt.Fprintln(s.out, "}")
	if c.ExtraInit != "" {
		fmt.Fprintf(s.out, "\n%s", c.ExtraInit)
	}
	fmt.Fprintln(s.out, "")
	fmt.Fprintf(s.out, "return s.%sDocument(s.doc %s)\n", generatedFunctionPrefix, s.ExtraWalkArgsInit)
	fmt.Fprintln(s.out, "}")
	fmt.Fprintln(s.out, "")

	oldEmptyCount := 0
	for {
		s.generateFuncs(documentType)
		emptyCount := 0
		for n := range s.funcs {
			if s.funcs[n] == "*" {
				emptyCount++
			} else {
				delete(s.funcs, n)
			}
		}
		if emptyCount == oldEmptyCount {
			break
		}
		oldEmptyCount = emptyCount
	}
	s.generateFuncs(documentType)
	fn := make([]string, 0, len(s.funcs))
	for n := range s.funcs {
		fn = append(fn, n)
	}
	sort.Strings(fn)
	for _, n := range fn {
		if s.funcs[n] != "*" {
			fmt.Fprint(s.out, s.funcs[n])
			fmt.Fprint(s.out, "\n\n")
		}
	}

	return s.out.Close()
}

func LoadPackage(root string) error {
	if pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedDeps | packages.NeedImports,
	}, path.Join(root, "internal/generator/openapi")); err != nil {
		return err
	} else {
		for _, pkg := range pkgs {
			if pkg.PkgPath == "github.com/ShouheiNishi/openapi5g/internal/generator/openapi" {
				if o := pkg.Types.Scope().Lookup("Document"); o == nil {
					return errors.New("no Document type")
				} else if tn, _ := o.(*types.TypeName); tn == nil {
					return errors.New("object Document is not type name")
				} else if t, _ := tn.Type().(*types.Named); t == nil {
					return errors.New("object Document is not named type")
				} else {
					documentType = t
					return nil
				}
			}
		}
		return errors.New("no openapi package")
	}
}

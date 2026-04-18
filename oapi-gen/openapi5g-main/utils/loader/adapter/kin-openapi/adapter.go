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

package kinopenapi

import (
	"net/url"
	"path"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/ShouheiNishi/openapi5g/utils/loader"
)

type state struct {
	doc  *openapi3.T
	err  error
	once sync.Once
}

var stateMap sync.Map

func GetDocument(loaderArg loader.SpecLoader) (*openapi3.T, error) {
	s := &state{}
	if value, loaded := stateMap.LoadOrStore(loaderArg, s); loaded {
		s = value.(*state)
	}
	s.once.Do(func() {
		kinLoader := openapi3.NewLoader()
		kinLoader.IsExternalRefsAllowed = true
		kinLoader.ReadFromURIFunc = func(_ *openapi3.Loader, url *url.URL) ([]byte, error) {
			return loaderArg.GetSpec(path.Base(url.Path))
		}
		s.doc, s.err = kinLoader.LoadFromFile(loaderArg.RootSpecName())
	})
	return s.doc, s.err
}

func GetDocumentMust(l loader.SpecLoader) *openapi3.T {
	doc, err := GetDocument(l)
	if err != nil {
		panic(err)
	}
	return doc
}

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

package pkgmap

import (
	"github.com/ShouheiNishi/openapi5g/utils/loader"
)

func SpecName2Loader(specName string) (loader loader.SpecLoader, exist bool) {
	if p, exist := SpecName2PkgName(specName); !exist {
		return nil, false
	} else {
		return PkgName2Loader(p)
	}
}

func SpecName2PkgName(specName string) (pkgName string, exist bool) {
	pkgName, exist = s2p[specName]
	return
}

func PkgName2Loader(pkgName string) (loader loader.SpecLoader, exist bool) {
	loader, exist = p2l[pkgName]
	return
}

func PkgName2specName(pkgName string) (specName string, exist bool) {
	specName, exist = p2s[pkgName]
	return
}

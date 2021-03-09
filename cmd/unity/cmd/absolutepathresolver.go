// Copyright 2021 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cmd

import (
	"path/filepath"
)

const (
	// commonPathBin is the name of the directory that will be used
	// at the module root of the directory specified in the absolute path
	// version
	commonPathBin = ".unity-bin"
)

// absolutePathResolver resolves a CUE version that is an absolute directory
// path, as uses the Go modules within that directory to resolve a CUE version.
// cue is then built within a .unity-bin directory at the Go module root
type absolutePathResolver struct {
	cp *commonPathResolver
}

func newAbsolutePathResolver(c resolverConfig) (resolver, error) {
	res := &absolutePathResolver{
		cp: c.commonPathResolver,
	}
	return res, nil
}

func (a *absolutePathResolver) resolve(version, dir, workingDir, target string) (string, error) {
	if !filepath.IsAbs(version) {
		return "", errNoMatch
	}
	return a.cp.resolve(version, target)
}

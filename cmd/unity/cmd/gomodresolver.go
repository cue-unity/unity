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

// goModResolver resolves a CUE version of "go.mod" and uses the Go module
// context within which the CUE module is found to resolve a CUE version. cue
// is then built within a .unity-bin directory at the Go module root
type goModResolver struct {
	cp *commonPathResolver
}

func newGoModResolver(c resolverConfig) (resolver, error) {
	res := &goModResolver{
		cp: c.commonPathResolver,
	}
	return res, nil
}

func (a *goModResolver) resolve(version, dir, workingDir, targetDir string) error {
	if version != "go.mod" {
		return errNoMatch
	}
	return a.cp.resolve(dir, targetDir)
}

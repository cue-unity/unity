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
	"fmt"
	"os/exec"
	"path/filepath"
)

// pathResolver resolves the CUE version "PATH" to the cue binary that is
// on PATH.
type pathResolver struct {
	config resolverConfig
}

var _ resolver = (*pathResolver)(nil)

func newPathResolver(c resolverConfig) (resolver, error) {
	res := &pathResolver{
		config: c,
	}
	return res, nil
}

func (p *pathResolver) resolve(version, dir, workingDir, targetDir string) error {
	if version != "PATH" {
		return errNoMatch
	}
	if !p.config.allowPATH {
		return errPATHNotAllowed
	}
	exe, err := exec.LookPath("cue")
	if err != nil {
		return fmt.Errorf("failed to find cue in PATH: %v", err)
	}
	// TODO: check GOOS and GOARCH for the result
	return copyExecutableFile(exe, filepath.Join(targetDir, "cue"))
}

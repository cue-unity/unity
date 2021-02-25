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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// absolutePathBin is the name of the directory that will be used
	// at the module root of the directory specified in the absolute path
	// version
	absolutePathBin = ".unity-bin"
)

type absolutePathResolver struct {
	config resolverConfig

	// roots is the builds we have completed, keyed by the module root.
	// We only attempt a build once per unity run
	roots map[string]*sync.Once

	// rootsLock guards roots
	rootsLock sync.Mutex
}

func newAbsolutePathResolver(c resolverConfig) (resolver, error) {
	res := &absolutePathResolver{
		config: c,
		roots:  make(map[string]*sync.Once),
	}
	return res, nil
}

func (a *absolutePathResolver) resolve(version, dir, workingDir, targetDir string) error {
	if !filepath.IsAbs(version) {
		return errNoMatch
	}
	// Find the module root
	goenv := exec.Command("go", "env", "GOMOD")
	goenv.Dir = version
	out, err := goenv.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to determine module root via [%v] in %s: %v\n%s", goenv, version, err, out)
	}
	gomod := strings.TrimSpace(string(out))
	if gomod == "" || gomod == os.DevNull {
		return fmt.Errorf("failed to resolve module root within %s: resolve %q", version, gomod)
	}
	root := filepath.Dir(gomod)
	bin := filepath.Join(root, absolutePathBin)
	if err := os.Mkdir(bin, 0777); err != nil {
		return fmt.Errorf("failed to create %s: %v", bin, err)
	}
	buildTarget := filepath.Join(bin, "cue")
	target := filepath.Join(targetDir, "cue")
	a.rootsLock.Lock()
	once, ok := a.roots[root]
	if !ok {
		once = new(sync.Once)
		a.roots[root] = once
	}
	var onceerr error
	once.Do(func() {
		onceerr = a.buildDir(version, buildTarget)
	})
	if onceerr != nil {
		return fmt.Errorf("failed to build CUE in %s: %v", root, onceerr)
	}
	return copyExecutableFile(buildTarget, target)
}

func (a *absolutePathResolver) buildDir(dir, target string) error {
	cmd := exec.Command("go", "build", "-o", target, "cuelang.org/go/cmd/cue")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), a.config.bh.buildEnv()...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run [%v] in %s: %v\n%s", cmd, dir, err, out)
	}
	return nil
}

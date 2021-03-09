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
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

type commonPathResolver struct {
	config resolverConfig

	// roots is the builds we have completed, keyed by the module root.
	// We only attempt a build once per unity run
	roots map[string]*sync.Once

	// rootsLock guards roots
	rootsLock sync.Mutex
}

func newCommonPathResolver(c resolverConfig) (*commonPathResolver, error) {
	res := &commonPathResolver{
		config: c,
		roots:  make(map[string]*sync.Once),
	}
	return res, nil
}

// resolve attempts to resolve cuelang.org/go as a Go dependency within
// dir. If cuelang.org/go is the main module, then the version returned
// is the commit found in that directory. Otherwise, the version of
// cuelang.org/go the dependency is returned.
func (a *commonPathResolver) resolve(dir, target string) (string, error) {
	cmd := exec.Command("go", "list", "-m", "-json")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to determine module information via [%v] in %s: %v\n%s", cmd, dir, err, out)
	}
	var gomod struct {
		Dir  string
		Path string
	}
	if err := json.Unmarshal(out, &gomod); err != nil {
		return "", fmt.Errorf("failed to parse module information: %v\n%s", err, out)
	}
	if gomod.Dir == "" {
		return "", fmt.Errorf("failed to resolve module root within %s: resolve %+v", dir, gomod)
	}
	root := gomod.Dir
	bin := filepath.Join(root, commonPathBin)
	if err := os.MkdirAll(bin, 0777); err != nil {
		return "", fmt.Errorf("failed to create %s: %v", bin, err)
	}
	buildTarget := filepath.Join(bin, "cue")
	a.rootsLock.Lock()
	defer a.rootsLock.Unlock()
	once, ok := a.roots[root]
	if !ok {
		once = new(sync.Once)
		a.roots[root] = once
	}
	var version string
	if gomod.Path == cueModule {
		commit, err := gitDir(dir, "rev-parse", "HEAD")
		if err != nil {
			return "", fmt.Errorf("failed to rev-parse HEAD: %v", err)
		}
		version = strings.TrimSpace(commit)
	} else {
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Version}}", cueModule)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to resolve version for module %s in %s: %v", cueModule, dir, err)
		}
		version = strings.TrimSpace(string(out))
	}
	var onceerr error
	once.Do(func() {
		onceerr = a.buildDir(dir, buildTarget)
	})
	if onceerr != nil {
		return "", fmt.Errorf("failed to build CUE in %s: %v", root, onceerr)
	}
	return version, copyExecutableFile(buildTarget, target)
}

func (a *commonPathResolver) buildDir(dir, target string) error {
	cmd := exec.Command("go", "build", "-o", target, cmdCue)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), a.config.bh.buildEnv()...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to run [%v] in %s: %v\n%s", cmd, dir, err, out)
	}
	return nil
}

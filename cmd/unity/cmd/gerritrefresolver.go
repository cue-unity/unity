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

	"github.com/rogpeppe/go-internal/lockedfile"
)

const (
	cueGitSource = "https://cue.googlesource.com/cue"
)

type gerritRefResolver struct {
	config resolverConfig

	// dir is the directory within which the CUE clone exists
	dir string

	// lock controls access to the user cache dir clone of CUE
	lock *lockedfile.Mutex
}

func newGerritRefResolver(c resolverConfig) (resolver, error) {
	dir := c.bh.cueCloneDir()
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, fmt.Errorf("failed to mkdir %s: %v", dir, err)
	}
	res := &gerritRefResolver{
		config: c,
		dir:    dir,
		lock:   lockedfile.MutexAt(dir + cloneLockfile),
	}
	return res, nil
}

func (g *gerritRefResolver) resolve(version, _, _, targetDir string) error {
	if !strings.HasPrefix(version, "refs/changes/") {
		return errNoMatch
	}

	target := filepath.Join(targetDir, "cue")

	// Check whether we have a cache hit
	h := g.config.bh.cueVersionHash(version)
	key := h.Sum()
	ce, _, err := g.config.bh.cache.GetFile(key)
	if err == nil {
		return copyExecutableFile(ce, target)
	}

	// We need to build
	unlock, err := g.lock.Lock()
	if err != nil {
		return fmt.Errorf("failed to acquire lockfile: %v", err)
	}
	defer unlock()

	// Ensure we have a clone in the first place
	if _, err := os.Stat(filepath.Join(g.dir, ".git")); err != nil {
		if _, err := gitDir(g.dir, "clone", cueGitSource, "."); err != nil {
			return fmt.Errorf("failed to clone CUE: %v", err)
		}
	}

	// fetch the version
	if _, err := gitDir(g.dir, "fetch", cueGitSource, version); err != nil {
		return fmt.Errorf("failed to fetch %s: %v", version, err)
	}

	// move to FETCH_HEAD
	if _, err := gitDir(g.dir, "checkout", "FETCH_HEAD"); err != nil {
		return fmt.Errorf("failed to checkout FETCH_HEAD: %v", err)
	}

	// build
	buildDir := filepath.Join(g.dir, "cmd", "cue")
	cmd := exec.Command("go", "build")
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(), g.config.bh.buildEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to run [%v] in %s: %v\n%s", cmd, buildDir, err, out)
	}
	buildTarget := filepath.Join(buildDir, "cue")

	f, err := os.Open(buildTarget)
	if err != nil {
		return fmt.Errorf("failed to open build result %s: %v", buildTarget, err)
	}
	if _, _, err := g.config.bh.cache.Put(key, f); err != nil {
		return fmt.Errorf("failed to write cue to the cache: %v", err)
	}

	return copyExecutableFile(buildTarget, target)
}

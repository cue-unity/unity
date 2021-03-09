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

	"github.com/rogpeppe/go-internal/lockedfile"
)

const (
	cueGitSource = "https://cue.googlesource.com/cue"
)

type commonCUEResolver struct {
	config resolverConfig

	// dir is the directory within which the CUE clone exists
	dir string

	// lock controls access to the user cache dir clone of CUE
	lock *lockedfile.Mutex
}

func newCommonCUEREsolver(c resolverConfig) (*commonCUEResolver, error) {
	dir := c.bh.cueCloneDir()
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, fmt.Errorf("failed to mkdir %s: %v", dir, err)
	}
	res := &commonCUEResolver{
		config: c,
		dir:    dir,
		lock:   lockedfile.MutexAt(dir + cloneLockfile),
	}
	return res, nil
}

func (c *commonCUEResolver) resolve(version, target string, strategy func(*commonCUEResolver) (string, error)) (string, error) {
	// Check whether we have a cache hit
	h := c.config.bh.cueVersionHash(version)
	ce, _, err := c.config.bh.cache.GetFile(h.Sum())
	if err == nil {
		// In this case the canonical version was specified so we can
		// return that directly
		return version, copyExecutableFile(ce, target)
	}

	// We need to build
	unlock, err := c.lock.Lock()
	if err != nil {
		return "", fmt.Errorf("failed to acquire lockfile: %v", err)
	}
	defer unlock()

	// Ensure we have a clone in the first place
	if _, err := os.Stat(filepath.Join(c.dir, ".git")); err != nil {
		if _, err := gitDir(c.dir, "clone", cueGitSource, "."); err != nil {
			return "", fmt.Errorf("failed to clone CUE: %v", err)
		}
	}

	version, err = strategy(c)
	if err != nil {
		return "", err
	}
	h = c.config.bh.cueVersionHash(version)

	// build
	buildDir := filepath.Join(c.dir, "cmd", "cue")
	cmd := exec.Command("go", "build")
	cmd.Dir = buildDir
	cmd.Env = append(os.Environ(), c.config.bh.buildEnv()...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to run [%v] in %s: %v\n%s", cmd, buildDir, err, out)
	}
	buildTarget := filepath.Join(buildDir, "cue")

	targetFile, err := os.Open(buildTarget)
	if err != nil {
		return "", fmt.Errorf("failed to open build result %s: %v", buildTarget, err)
	}
	defer targetFile.Close()
	if _, _, err := c.config.bh.cache.Put(h.Sum(), targetFile); err != nil {
		return "", fmt.Errorf("failed to write cue to the cache: %v", err)
	}

	return version, copyExecutableFile(buildTarget, target)
}

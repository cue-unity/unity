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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cue-unity/unity/internal/cuetest"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

// TestScripts runs testscript txtar tests that test unity
func TestScripts(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// selfDir is the path that contains the unity command for running
	// in tests
	bh, err := newBuildHelper()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bh.cache.Trim)
	if err := bh.targetDocker(dockerImage); err != nil {
		t.Fatal(err)
	}
	// This will build self (i.e. unity) into $modroot/.bin.
	// Note that we don't install the tool via testscript.RunMain.
	selfPath, err := bh.pathToSelf(cwd, "")
	if err != nil {
		t.Fatal(err)
	}
	selfDir := filepath.Dir(selfPath)
	cueTarget := filepath.Join(selfDir, "cue")
	// install the required version of CUE to ensure that CUE versions of PATH
	// specified in unity tests run consistently for the target docker image
	if err := bh.buildAndCache(cwd, cueTarget, cmdCue); err != nil {
		t.Fatal(err)
	}

	var goEnv struct {
		GOCACHE    string
		GOMODCACHE string
	}
	goenv := exec.Command("go", "env", "-json")
	out, err := goenv.CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &goEnv); err != nil {
		t.Fatal(err)
	}

	for _, unityUnsafe := range []bool{false, true} {
		unityUnsafe := unityUnsafe
		t.Run(fmt.Sprintf("unsafe=%t", unityUnsafe), func(t *testing.T) {
			t.Parallel()
			testscript.Run(t, testscript.Params{
				Dir: filepath.Join("testdata", "scripts"),
				Setup: func(env *testscript.Env) (err error) {
					defer helperDefer(&err)
					h := &helper{env: env}

					// Augment the environment with a HOME setup
					const homeDirName = ".user-home"
					home := filepath.Join(env.WorkDir, homeDirName)
					if err := os.Mkdir(home, 0777); err != nil {
						return err
					}

					// Augment the environment
					env.Setenv("GOCACHE", goEnv.GOCACHE)
					env.Setenv("GOMODCACHE", goEnv.GOMODCACHE)
					env.Setenv("PATH", selfDir+string(os.PathListSeparator)+env.Getenv("PATH"))
					env.Setenv(homeEnvName(), home)
					env.Setenv("UNITY_SEMVER_URL_TEMPLATE", "file://"+filepath.Join(cwd, "testdata", "archives", "{{.Artefact}}"))
					env.Setenv("UNITY_UNSAFE", fmt.Sprintf("%t", unityUnsafe))
					env.Setenv("UNITY_TESTSCRIPT", "true")

					// Always run git config steps
					h.git("config", "--global", "user.name", "unity")
					h.git("config", "--global", "user.email", "unity@cuelang.org")
					gitignore := filepath.Join(home, "gitignore")
					h.write(gitignore, homeDirName+"\n")
					h.git("config", "--global", "core.excludesfile", gitignore)
					h.git("config", "--global", "init.defaultBranch", "main")

					// Pre-script setup via special files
					if err := processSpecialFiles(env); err != nil {
						return err
					}
					return nil
				},
				Condition:           cuetest.Condition,
				RequireExplicitExec: true,
			})
		})
	}
}

const specialUnquote = ".unquote"

// processSpecialFiles performs pre-script setup using the existence of
// special files to drive what should be done
func processSpecialFiles(env *testscript.Env) (err error) {
	defer helperDefer(&err)
	h := &helper{env: env}
	// Do unquoting first
	h.walk(specialUnquote, func(path string) {
		files := h.speciallines(h.read(path))
		for _, fn := range files {
			f := filepath.Join(filepath.Dir(path), fn)
			c := h.read(f)
			u, err := txtar.Unquote([]byte(c))
			h.check(err, "failed to unquote %s: %v", fn, err)
			h.write(f, string(u))
		}
	})
	return nil
}

type helper struct {
	env *testscript.Env
}

func helperDefer(err *error) {
	switch r := recover().(type) {
	case nil:
	case runtime.Error:
		panic(r)
	case error:
		*err = r
	}
}

func (h *helper) walk(base string, f func(dir string)) {
	err := filepath.Walk(h.env.WorkDir, func(path string, info fs.FileInfo, err error) error {
		if !info.Mode().IsRegular() {
			return nil
		}
		if filepath.Base(path) != base {
			return nil
		}
		f(path)
		return nil
	})
	check(err, "failed to walk for basename %s: %v", base, err)
}

func (h *helper) check(err error, format string, args ...interface{}) {
	if err != nil {
		panic(fmt.Errorf(format, args...))
	}
}

func (h *helper) read(f string) string {
	if !filepath.IsAbs(f) {
		f = filepath.Join(h.env.WorkDir, f)
	}
	c, err := os.ReadFile(f)
	h.check(err, "failed to read contents of %s: %v", f, err)
	return string(c)
}

func (h *helper) write(f string, c string) {
	if !filepath.IsAbs(f) {
		f = filepath.Join(h.env.WorkDir, f)
	}
	err := os.WriteFile(f, []byte(c), 0666)
	h.check(err, "failed to write to %s: %v", f, err)
}

func (h *helper) speciallines(c string) (res []string) {
	lines := strings.Split(c, "\n")
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "#") {
			res = append(res, l)
		}
	}
	return res
}

func (h *helper) git(args ...string) string {
	return h.gitDir(h.env.WorkDir, args...)
}

func (h *helper) gitDir(dir string, args ...string) string {
	res, err := gitEnvDir(h.env.Vars, dir, args...)
	if err != nil {
		panic(err)
	}
	return res
}

func homeEnvName() string {
	switch runtime.GOOS {
	case "windows":
		return "USERPROFILE"
	case "plan9":
		return "home"
	default:
		return "HOME"
	}
}

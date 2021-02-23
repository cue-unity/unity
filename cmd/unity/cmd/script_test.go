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
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

const (
	homeDirName = ".user-home"
)

func TestMain(m *testing.M) {
	os.Exit(testscript.RunMain(m, map[string]func() int{
		"unity": MainTest,
	}))
}

// TestScripts runs testscript txtar tests that test unity
func TestScripts(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	// selfDir is the path that contains the unity command for running
	// in tests
	selfDir, ok, err := pathToSelf(cwd)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("pathToSelf should not be temporary in a test scenario")
	}
	// go install the required version of CUE to ensure that CUE versions
	// of PATH specified in unity tests run consistently. Put this
	// alongside unity in selfDir
	cmd := exec.Command("go", "install", "cuelang.org/go/cmd/cue")
	cmd.Env = append(os.Environ(), "GOBIN="+selfDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to run %v: %v\n%s", cmd, err, out)
	}

	for _, v := range []string{"safe", "unsafe"} {
		v := v
		t.Run(v, func(t *testing.T) {
			testscript.Run(t, testscript.Params{
				Dir: filepath.Join("testdata", "scripts"),
				Setup: func(e *testscript.Env) (err error) {
					defer helperDefer(&err)
					h := &helper{env: e}

					// Augment the environment with a HOME setup
					home := filepath.Join(e.WorkDir, homeDirName)
					if err := os.Mkdir(home, 0777); err != nil {
						return err
					}

					// Add GOBIN (set above) to PATH
					var path string
					for i := len(e.Vars) - 1; i >= 0; i-- {
						v := e.Vars[i]
						if strings.HasPrefix(v, "PATH=") {
							path = strings.TrimPrefix(v, "PATH=")
							break
						}
					}
					path = selfDir + string(os.PathListSeparator) + path

					// Augment the environment
					e.Vars = append(e.Vars,
						"UNITY_TEST_PATH_TO_SELF="+selfDir,
						"PATH="+path,
						"HOME="+home,
						"UNITY_SEMVER_URL_TEMPLATE=file://"+filepath.Join(cwd, "testdata", "archives", "{{.Artefact}}"),
					)
					if v == "unsafe" {
						e.Vars = append(e.Vars,
							"UNITY_UNSAFE=true",
						)
					}

					// Always run git config steps
					h.git("config", "--global", "user.name", "unity")
					h.git("config", "--global", "user.email", "unity@cuelang.org")
					h.git("config", "--global", "user.email", "unity@cuelang.org")
					h.write(filepath.Join(home, ".gitignore"), strings.Join([]string{
						homeDirName,
					}, "\n"))
					h.git("config", "--global", "core.excludesfile", filepath.Join(home, ".gitignore"))
					h.git("config", "--global", "init.defaultBranch", "main")

					// Pre-script setup via special files
					if err := processSpecialFiles(e); err != nil {
						return err
					}
					return nil
				},
				Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
					"cue": runCmd("cue"),
					"git": runCmd("git"),
				},
			})
		})
	}
}

func runCmd(cmd string) func(ts *testscript.TestScript, neg bool, args []string) {
	return func(ts *testscript.TestScript, neg bool, args []string) {
		err := ts.Exec(cmd, args...)
		if err != nil {
			ts.Logf("[%v]\n", err)
			if !neg {
				ts.Fatalf("unexpected %s command failure", cmd)
			}
		} else {
			if neg {
				ts.Fatalf("unexpected %s command success", cmd)
			}
		}
	}
}

const (
	specialUnquote = ".unquote"
)

// processSpecialFiles performs pre-script setup using the existence of
// special files to drive what should be done
func processSpecialFiles(e *testscript.Env) (err error) {
	defer helperDefer(&err)
	h := &helper{env: e}
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

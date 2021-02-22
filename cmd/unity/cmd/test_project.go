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
	"bytes"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"github.com/cue-sh/unity"
	"github.com/rogpeppe/go-internal/testscript"
	"github.com/rogpeppe/go-internal/txtar"
)

const (
	// repoDir is the directory within a testscript Workdir to which
	// we create a worktree copy of the project under test. The
	// initial working directory for the CUE module under test is
	// then $WORK/repo/path/to/mod
	repoDir = "repo"
)

var (
	errTestFail = errors.New("tests failed")
)

func testProject(pt *projectTester, dir string, versions []string) error {
	p, err := pt.newInstance(dir)
	if err != nil {
		return err
	}
	done := make(map[string]bool)

	// At this stage, we know that toTest is a list of
	// valid and fully resolved versions to test
	type testResult struct {
		log *bytes.Buffer
		err error
	}
	var tested []*testResult
	verify := func(toTest []string) {
		var wg sync.WaitGroup
		for _, v := range toTest {
			v := v
			if done[v] {
				continue
			}
			done[v] = true
			res := &testResult{
				log: new(bytes.Buffer),
			}
			tested = append(tested, res)
			wg.Add(1)
			go func() {
				defer wg.Done()
				res.err = p.run(res.log, v)
			}()
		}
		wg.Wait()
	}
	// First check the base versions
	verify(p.manifest.Versions)
	sawError := false
	for _, tr := range tested {
		if tr.err != nil {
			sawError = true
		}
	}
	// Only run the additional versions if we passed the base version
	if !sawError {
		verify(versions)
	}

	// Subjective error printing. Log errors that are non errTestFail
	// first, then if we had any test failures dump the logs. If
	// we saw any errors return errTestFail
	for _, tr := range tested {
		if tr.err != nil && !errors.Is(tr.err, errTestFail) {
			sawError = true
			fmt.Fprintln(os.Stderr, tr.err)
		}
	}
	for _, tr := range tested {
		if tr.err != nil && errors.Is(tr.err, errTestFail) {
			sawError = true
			fmt.Fprint(os.Stderr, tr.log.String())
		}
	}
	if sawError {
		return errTestFail
	}
	return nil
}

type projectTester struct {
	// versionResolver is the helper to resolve CUE versions for testing
	versionResolver *versionResolver

	runtime *cue.Runtime

	// manifestDef is the CUE definition from the unity package
	manifestDef cue.Value

	// semaphore controls concurrency levels in projet tests
	semaphore chan struct{}

	verbose bool
}

func newProjectTester(vr *versionResolver, r *cue.Runtime, manifestDef cue.Value) *projectTester {
	sem := make(chan struct{}, runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		sem <- struct{}{}
	}
	pt := &projectTester{
		versionResolver: vr,
		runtime:         r,
		manifestDef:     manifestDef,
		semaphore:       sem,
	}
	return pt
}

// limit returns blocks until a concurrency slot is available
// for execution, and then returns a function which can be used
// in a defer to release the semaphore.
func (pt *projectTester) limit() func() {
	<-pt.semaphore
	return func() {
		pt.semaphore <- struct{}{}
	}
}

func (pt *projectTester) newInstance(dir string) (*project, error) {
	mod := load.Instances([]string{"."}, &load.Config{Dir: dir})[0]
	if mod.Module == "" {
		return nil, fmt.Errorf("could not find main CUE module root")
	}

	// Find the git root. Run this from the working directory in case
	// the CUE main module is not contained within a git directory
	// (which we check below)
	gitRoot, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("failed to determine git root: %v", err)
	}
	gitRoot = strings.TrimSpace(gitRoot)

	// Verify that the CUE main module exists within the git dir
	relPath, err := filepath.Rel(gitRoot, mod.Root)
	if err != nil {
		return nil, fmt.Errorf("failed to determine main module root relative to git root: %v", err)
	}
	if strings.HasPrefix(relPath, "..") {
		return nil, fmt.Errorf("main CUE module root %q is not contained within git repository %q", mod.Root, gitRoot)
	}

	// Until we support a "dirty" mode we need to bail on a non-porcelain
	// git setup
	status, err := git("status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("failed to determine if git working tree status")
	}
	if strings.TrimSpace(status) != "" {
		return nil, fmt.Errorf("working tree is dirty; not currently supported: %v", status)
	}

	// Verify this is a valid project by loading the manifest
	manifestDir := filepath.Join(mod.Root, "cue.mod", "tests")
	manifestInst := load.Instances([]string{"."}, &load.Config{Dir: manifestDir})
	manifestInput, err := pt.runtime.Build(manifestInst[0])
	if err != nil {
		return nil, fmt.Errorf("failed to load tests manifest from %s: %v", manifestDir, err)
	}

	// Validate against the embedded #Manifest definition
	manifestVal := pt.manifestDef.Unify(manifestInput.Value())
	if err := manifestVal.Validate(cue.Concrete(true)); err != nil {
		return nil, fmt.Errorf("failed to validate tests manifest: %v", err)
	}
	var manifest unity.Manifest
	if err := manifestVal.Decode(&manifest); err != nil {
		return nil, fmt.Errorf("failed to decode manifest: %v", err)
	}

	// Pre-validate the CUE versions
	//
	// TODO: make concurrent
	for _, v := range manifest.Versions {
		_, err := pt.versionResolver.resolve(v)
		if err != nil {
			return nil, err
		}
	}

	// Pre-validate that none of the testscript files we are going to validate
	// have a module/ path in their archive
	scripts, err := filepath.Glob(filepath.Join(manifestDir, "*.txt"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob for input scripts: %v", err)
	}
	for _, s := range scripts {
		archive, err := txtar.ParseFile(s)
		if err != nil {
			return nil, fmt.Errorf("failed to parse txtar archive %s: %v", s, err)
		}
		for _, f := range archive.Files {
			p := path.Clean(f.Name)
			if p == repoDir || strings.Split(p, "/")[0] == repoDir {
				return nil, fmt.Errorf("%s contains invalid file path %s", s, f.Name)
			}
		}
	}

	res := &project{
		tester:      pt,
		gitRoot:     gitRoot,
		modRoot:     mod.Root,
		relPath:     relPath,
		manifestDir: manifestDir,
		manifest:    manifest,
	}
	return res, nil
}

type project struct {
	// modRoot is the absolute path to the module root
	// The CUE module will be contained within gitroot
	modRoot string

	// gitRoot is the absolute path to the git root that
	// contains modroot.
	gitRoot string

	// relPath is a convenience calculation of modpath
	// relative to gitroot
	relPath string

	// manifestDir is the absolute path to the manifest
	// directory within a CUE module
	manifestDir string

	// manifest is the decoded manifest for the project
	manifest unity.Manifest

	// tester is the projectTester instance that created
	// this project instance
	tester *projectTester
}

func (p *project) run(log *bytes.Buffer, version string) (err error) {
	path, err := p.tester.versionResolver.resolve(version)
	if err != nil {
		return err
	}
	params := testscript.Params{
		Dir: p.manifestDir,
		Setup: func(e *testscript.Env) error {
			// Limit concurrency across all testscript runs
			e.Defer(p.tester.limit())

			// Make a copy of the current state of the git repo into
			// into the repo subdirectory of the workdir
			modCopy := filepath.Join(e.WorkDir, repoDir)
			_, err = gitDir(p.gitRoot, "worktree", "add", "-d", modCopy)
			if err != nil {
				return fmt.Errorf("failed to create copy of current HEAD: %v", err)
			}
			e.Defer(func() {
				gitDir(p.gitRoot, "worktree", "remove", modCopy)
			})
			// Set the working directory to be module
			e.Cd = filepath.Join(e.WorkDir, repoDir, p.relPath)
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"cue": buildCmdCUE(path),
		},
	}
	// TODO: improve logging/printing/errors when we make things concurrent
	r := newRunT("", nil)
	func() {
		defer func() {
			switch recover() {
			case nil, skipRun, failedRun:
				// normal operation
			default:
				panic(err)
			}
		}()
		testscript.RunT(r, params)
	}()
	if r.failed && len(r.children) == 0 {
		// We failed before running any subtests
		return errors.New(r.log.String())
	}
	sort.Slice(r.children, func(i, j int) bool {
		lhs, rhs := r.children[i], r.children[j]
		return lhs.name < rhs.name
	})
	for _, c := range r.children {
		if !c.failed && !c.verbose {
			continue
		}
		passFail := "PASS"
		if c.failed {
			passFail = "FAIL"
		}
		fmt.Fprintf(log, "--- %s: %s/%s\n%s", passFail, c.name, version, indent(c.log, "\t"))
	}
	if r.failed {
		return errTestFail
	}
	return nil
}

// indent returns the indented string version of b
func indent(b *bytes.Buffer, indent string) string {
	s := b.String()
	var trailing bool
	if s != "" && s[len(s)-1] == '\n' {
		trailing = true
		s = s[:len(s)-1]
	}
	s = indent + strings.ReplaceAll(s, "\n", "\n"+indent)
	if trailing {
		s += "\n"
	}
	return s
}

func buildCmdCUE(path string) func(ts *testscript.TestScript, neg bool, args []string) {
	return func(ts *testscript.TestScript, neg bool, args []string) {
		if len(args) < 1 {
			ts.Fatalf("usage: cue subcommand ...")
		}
		err := ts.Exec(path, args...)
		if err != nil {
			ts.Logf("[%v]\n", err)
			if !neg {
				ts.Fatalf("unexpected cue command failure")
			}
		} else {
			if neg {
				ts.Fatalf("unexpected cue command success")
			}
		}
	}
}

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
	"io/fs"
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
	// we create a worktree copy of the module under test. The
	// initial working directory for the CUE module under test is
	// then $WORK/repo/path/to/mod
	repoDir = "repo"

	// packageTests is the name of the package within which we define
	// the module test manifest and the testscript files
	packageTests = "tests"
)

var (
	errTestFail = errors.New("tests failed")
)

func testProject(cmd *Command, mt *moduleTester, versions []string) error {
	modules, err := mt.deriveModules(mt.gitRoot)
	if err != nil {
		return fmt.Errorf("failed to derive modules under %s: %v", mt.gitRoot, err)
	}
	if len(modules) == 0 {
		return fmt.Errorf("could not find any CUE module roots")
	}
	return mt.test(modules, versions)
}

func (mt *moduleTester) test(modules []*module, versions []string) error {
	done := make(map[*module]map[string]bool)

	// At this stage, we know that toTest is a list of
	// valid and fully resolved versions to test
	type testResult struct {
		log *bytes.Buffer
		err error
	}
	var tested []*testResult
	verify := func(whatToTest func(*module) []string) {
		var wg sync.WaitGroup
		for _, m := range modules {
			m := m
			mdone := done[m]
			if mdone == nil {
				mdone = make(map[string]bool)
				done[m] = mdone
			}
			toTest := whatToTest(m)
			for _, v := range toTest {
				v := v
				if mdone[v] {
					continue
				}
				mdone[v] = true
				res := &testResult{
					log: new(bytes.Buffer),
				}
				tested = append(tested, res)
				wg.Add(1)
				go func() {
					defer wg.Done()
					res.err = mt.run(m, res.log, v)
				}()
			}
		}
		wg.Wait()
	}
	// First check the base versions
	verify(func(m *module) []string { return m.manifest.Versions })
	sawError := false
	for _, tr := range tested {
		if tr.err != nil {
			sawError = true
		}
	}
	// Only run the additional versions if we passed the base version
	if !sawError && len(versions) > 0 {
		verify(func(*module) []string { return versions })
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

type moduleTester struct {
	// versionResolver is the helper to resolve CUE versions for testing
	versionResolver *versionResolver

	runtime *cue.Runtime

	// manifestDef is the CUE definition from the unity package
	manifestDef cue.Value

	// semaphore controls concurrency levels in projet tests
	semaphore chan struct{}

	// gitRoot is the absolute path to the git root used as the context
	// for testing modules. In -corpus mode this will be the git top level
	// of the repo that contains the git submodules. In project mode (default)
	// this will be the git top level of the project repository that will
	// be searched for CUE modules.
	gitRoot string

	// overlayDir is a directory that might contain overlays for a
	// given module.
	overlayDir string

	verbose bool
}

func newModuleTester(gitRoot, overlayDir string, vr *versionResolver, r *cue.Runtime, manifestDef cue.Value) *moduleTester {
	sem := make(chan struct{}, runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		sem <- struct{}{}
	}
	mt := &moduleTester{
		overlayDir:      overlayDir,
		gitRoot:         gitRoot,
		versionResolver: vr,
		runtime:         r,
		manifestDef:     manifestDef,
		semaphore:       sem,
	}
	return mt
}

// limit returns blocks until a concurrency slot is available
// for execution, and then returns a function which can be used
// in a defer to release the semaphore.
func (mt *moduleTester) limit() func() {
	<-mt.semaphore
	return func() {
		mt.semaphore <- struct{}{}
	}
}

// newInstance creates a module instances rooted in the CUE module that is dir.
// A precondition of this function is that dir must be contained in gitRoot.
func (mt *moduleTester) newInstance(gitRoot, dir string) (*module, error) {
	mod := load.Instances([]string{"."}, &load.Config{Dir: dir})[0]
	if mod.Module == "" {
		return nil, fmt.Errorf("could not find main CUE module root")
	}

	// We know that dir is contained within gitRoot. Furthermore, that gitRoot is
	// contained within mt.gitRoot. Store the relative paths on the resulting
	// module for convenience
	testerGitRel := dir[len(mt.gitRoot):]
	if strings.HasPrefix(testerGitRel, string(os.PathSeparator)) {
		testerGitRel = strings.TrimPrefix(testerGitRel, string(os.PathSeparator))
	}
	gitRel := dir[len(gitRoot):]
	if strings.HasPrefix(gitRel, string(os.PathSeparator)) {
		gitRel = strings.TrimPrefix(gitRel, string(os.PathSeparator))
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

	// Verify this is a valid module by loading the manifest
	manifestDir := filepath.Join(mod.Root, "cue.mod", packageTests)
	// Now see if there is an overlay for this path
	// Only if the tests directory exists do we attempt to
	// create a module instance
	if mt.overlayDir != "" {
		overlayDir := filepath.Join(mt.overlayDir, testerGitRel)
		if fi, err := os.Stat(overlayDir); err == nil && fi.IsDir() {
			manifestDir = overlayDir
		}
	}
	manifestInst := load.Instances([]string{"."}, &load.Config{
		Dir: manifestDir,
	})
	manifestInput, err := mt.runtime.Build(manifestInst[0])
	if err != nil {
		return nil, fmt.Errorf("failed to load tests manifest from %s: %v", manifestDir, err)
	}

	// Validate against the embedded #Manifest definition
	manifestVal := mt.manifestDef.Unify(manifestInput.Value())
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
		_, err := mt.versionResolver.resolve(v)
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

	res := &module{
		tester:        mt,
		gitRoot:       gitRoot,
		root:          mod.Root,
		testerRelPath: testerGitRel,
		relPath:       gitRel,
		manifestDir:   manifestDir,
		manifest:      manifest,
	}
	return res, nil
}

func (mt *moduleTester) deriveModules(dir string) (modules []*module, err error) {
	err = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.Name() != "cue.mod" {
			return nil
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", path)
		}
		modDir := filepath.Dir(path)
		m, err := mt.newInstance(dir, modDir)
		if err != nil {
			return fmt.Errorf("failed to create module instance at %s: %v", modDir, err)
		}
		modules = append(modules, m)
		return nil
	})
	return
}

// module represents a CUE module under test
type module struct {
	// root is the absolute path to the module root
	// The CUE module will be contained within gitroot
	root string

	// gitRoot is the absolute path to the git root that
	// contains modroot.
	gitRoot string

	// testerRelPath is a convenience calculation of modpath relative to the
	// gitRoot of the moduleTester that created the module
	testerRelPath string

	// relPath is a convenience calculation of modpath
	// relative to gitRoot (that is the project's git root)
	relPath string

	// manifestDir is the absolute path to the manifest
	// directory within a CUE module
	manifestDir string

	// manifest is the decoded manifest for the module
	manifest unity.Manifest

	// tester is the moduleTester instance that created
	// this module instance
	tester *moduleTester
}

func (mt *moduleTester) run(m *module, log *bytes.Buffer, version string) (err error) {
	cuePath, err := m.tester.versionResolver.resolve(version)
	if err != nil {
		return err
	}
	params := testscript.Params{
		Dir: m.manifestDir,
		Setup: func(e *testscript.Env) error {
			// Limit concurrency across all testscript runs
			e.Defer(m.tester.limit())

			// Make a copy of the current state of the git repo into
			// into the repo subdirectory of the workdir
			modCopy := filepath.Join(e.WorkDir, repoDir)
			_, err = gitDir(m.gitRoot, "worktree", "add", "-d", modCopy)
			if err != nil {
				return fmt.Errorf("failed to create copy of current HEAD: %v", err)
			}
			e.Defer(func() {
				gitDir(m.gitRoot, "worktree", "remove", modCopy)
			})
			// Set the working directory to be module
			e.Cd = filepath.Join(e.WorkDir, repoDir, m.relPath)
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"cue": buildCmdCUE(cuePath),
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
		context := mt.buildContext(m, c.name, version)
		if !c.failed && !c.verbose {
			continue
		}
		passFail := "PASS"
		if c.failed {
			passFail = "FAIL"
		}
		fmt.Fprintf(log, "--- %s: %s\n%s", passFail, context, indent(c.log, "\t"))
	}
	if r.failed {
		return errTestFail
	}
	return nil
}

func (mt *moduleTester) buildContext(m *module, vs ...string) string {
	var context []string
	if m.testerRelPath != "" {
		context = append(context, m.testerRelPath)
	}
	context = append(context, vs...)
	return path.Join(context...)
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

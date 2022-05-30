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
	"io"
	"io/fs"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"github.com/cue-unity/unity"
	"github.com/olekukonko/tablewriter"
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

	firstResult := make(map[*module]*testResult)

	// At this stage, we know that toTest is a list of
	// valid and fully resolved versions to test
	var tested []*testResult
	verify := func(allowUpdate bool, whatToTest func(*module) []string) {
		// var wg sync.WaitGroup
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
					log:     new(bytes.Buffer),
					module:  m,
					version: v,
				}
				if _, ok := firstResult[m]; !ok {
					firstResult[m] = res
				}
				tested = append(tested, res)
				// wg.Add(1)
				// go func() {
				// 	defer wg.Done()
				res.err = mt.run(res, allowUpdate)
				// }()
			}
		}
		// wg.Wait()
	}

	// Write results to a table
	tw := tablewriter.NewWriter(os.Stderr)
	tw.SetAutoWrapText(false)
	tw.SetAutoFormatHeaders(true)
	tw.SetHeaderAlignment(tablewriter.ALIGN_LEFT)
	tw.SetColumnAlignment([]int{tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_LEFT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_RIGHT, tablewriter.ALIGN_LEFT})
	tw.SetCenterSeparator("")
	tw.SetColumnSeparator("")
	tw.SetRowSeparator("")
	tw.SetHeaderLine(false)
	tw.SetBorder(false)
	tw.SetTablePadding("  ")
	tw.SetNoWhiteSpace(true)

	// The logic on when we allow updates is driven by the versions that may
	// have been passed as arguments. With no versions supplied as arguments
	// we allow updating for the base versions of a module (ignoring the
	// fact that there might be conflicts between multiple versions). With
	// a single version supplied as an argument, we don't allow updating
	// the base versions, but do the supplied version. Otherwise, updating is
	// not permitted.

	// First check the base versions
	verify(len(versions) == 0, func(m *module) []string { return m.manifest.Versions })
	sawError := false
	for _, tr := range tested {
		if tr.err != nil {
			sawError = true
		}
	}
	// Only run the additional versions if the base version passed with
	// no failures
	if !sawError && len(versions) > 0 {
		verify(len(versions) == 1, func(*module) []string { return versions })
	}

	logTime := func(tr *testResult) {
		status := "ok"
		if tr.err != nil {
			status = "FAIL"
		}
		prev := firstResult[tr.module]
		v, p := float64(tr.duration), float64(prev.duration)
		var diff, prevVersion string
		if prev != tr {
			diff = fmt.Sprintf("%+.3f%%", (v-p)/p*100)
			prevVersion = prev.resolvedVersion
		}
		tw.Append([]string{status, tr.module.path, tr.resolvedVersion, fmt.Sprintf("%.3fs", tr.duration.Seconds()), diff, prevVersion})
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
	out := os.Stderr
	if mt.verbose {
		out = os.Stdout
	}
	for _, tr := range tested {
		hasErr := tr.err != nil && errors.Is(tr.err, errTestFail)
		sawError = sawError || hasErr
		if hasErr || mt.verbose {
			fmt.Fprint(out, tr.log.String())
		}
		logTime(tr)
	}
	tw.Render()
	if sawError {
		return errTestFail
	}
	return nil
}

type testResult struct {
	module          *module
	version         string
	resolvedVersion string
	log             *bytes.Buffer
	err             error
	duration        time.Duration
}

type moduleTester struct {
	// self is the path to the compiled version of self to be run within
	// a docker container
	self string

	// buildHelper is the build helper we use for all binary builds. It
	// encapsulates the target GOOS and GOARCH, and nicely bundles up
	// the logic required for saving binary assets into the unity cache
	// if required
	buildHelper *buildHelper

	// image is the docker image to use for safe testing
	image string

	// versionResolver is the helper to resolve CUE versions for testing
	versionResolver *versionResolver

	runtime *cue.Context

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

	// unsafe indicates that we are allowed to run scripts tests in-process
	// as opposed to in a separate Docker container
	unsafe bool

	// update files within test archives when a cmp fails
	update bool

	// staged indicates that staged git changes should be applied
	// when making a worktree copy of a module's project
	staged bool

	// ignoreDirty indicates we should ignore untracked files when making
	// a copy of a module's project
	ignoreDirty bool

	// cwd is the working directory in which the module tester is being run
	// stored for convenience
	cwd string

	// working is the temporary directory that moduleTester uses for temporary
	// files. Call cleanup to remove this and anything contained within it
	working string
}

func newModuleTester(mt moduleTester) (*moduleTester, error) {
	sem := make(chan struct{}, runtime.NumCPU())
	for i := 0; i < runtime.NumCPU(); i++ {
		sem <- struct{}{}
	}
	mt.semaphore = sem
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to derive working directory: %v", err)
	}
	td, err := ioutil.TempDir("", "unity-module-tester")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary working directory: %v", err)
	}
	mt.cwd = cwd
	mt.working = td
	return &mt, nil
}

// limit returns blocks until a concurrency slot is available
// for execution, and then returns a function which can be used
// in a defer to release the semaphore.
// func (mt *moduleTester) limit() func() {
// 	<-mt.semaphore
// 	return func() {
// 		mt.semaphore <- struct{}{}
// 	}
// }

// cleanup removes the temporary working directory of mt
func (mt *moduleTester) cleanup() error {
	return os.RemoveAll(mt.working)
}

// tempDir returns a temporary directory within the working temporary directory
func (mt *moduleTester) tempDir(name string) (string, error) {
	return ioutil.TempDir(mt.working, name)
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

	// Verify git status
	hasStaged, err := mt.verifyGitStatus(gitRoot)
	if err != nil {
		return nil, err
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
	manifestInput := mt.runtime.BuildInstance(manifestInst[0])
	if err := manifestInput.Err(); err != nil {
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
		path:          mod.Module,
		tester:        mt,
		gitRoot:       gitRoot,
		root:          mod.Root,
		testerRelPath: testerGitRel,
		relPath:       gitRel,
		manifestDir:   manifestDir,
		scripts:       scripts,
		manifest:      manifest,
		hasStaged:     hasStaged,
	}
	return res, nil
}

// verifyGitStatus ensures that the working tree in dir is valid according to
// the configuration of mt. It returns hasStaged to indicate if there are
// staged changes.
func (mt *moduleTester) verifyGitStatus(dir string) (hasStaged bool, err error) {
	status, err := gitDir(dir, "status", "--porcelain")
	if err != nil {
		return false, fmt.Errorf("failed to determine if git working tree status")
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return false, nil
	}
	lines := strings.Split(status, "\n")
	hasUntracked := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "??"):
			hasUntracked = true
		case l[0] != ' ':
			hasStaged = true
		}
	}
	if mt.ignoreDirty {
		// Nothing to check, but note we return the staged status
		return
	}
	if hasUntracked {
		return hasStaged, fmt.Errorf("working tree has untracked files; stage changes and use --%s or use --%s", flagTestStaged, flagTestIgnoreDirty)
	}
	// So we now don't have untracked changes but do have staged changes
	if !mt.staged {
		return hasStaged, fmt.Errorf("working tree has staged changes; use --%s to test with staged changes", flagTestStaged)
	}
	return
}

func (mt *moduleTester) deriveModules(dir string) (modules []*module, err error) {
	err = filepath.Walk(dir, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (strings.HasPrefix(info.Name(), ".") || strings.HasPrefix(info.Name(), "_")) {
			return filepath.SkipDir
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
		// Do not recurse within the cue.mod - otherwise we might find modules
		// in the vendor
		return filepath.SkipDir
	})
	return
}

// module represents a CUE module under test
type module struct {
	// path is the module path
	path string

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

	// scripts is the list of .txt files that will be run
	// as testscript tests for this module
	scripts []string

	// manifest is the decoded manifest for the module
	manifest unity.Manifest

	// tester is the moduleTester instance that created
	// this module instance
	tester *moduleTester

	// hasStaged indicates the module is part of a project
	// that has hasStaged git changes that should be applied
	hasStaged bool
}

func (mt *moduleTester) run(tr *testResult, allowUpdate bool) (err error) {
	m := tr.module
	version := tr.version
	// TODO: we really shouldn't need to be resolving this again
	working, err := mt.tempDir("run-dir")
	if err != nil {
		return fmt.Errorf("failed to create temp directory for run: %v", err)
	}
	cuePath := filepath.Join(working, ".cuebin", "cue")
	// Create the cuePath containing directory
	if err := os.Mkdir(filepath.Dir(cuePath), 0777); err != nil {
		return fmt.Errorf("failed to create cue bin directory for %q: %v", cuePath, err)
	}
	tr.resolvedVersion, err = m.tester.versionResolver.resolve(version, m.root, working, cuePath)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "testing %s against version %s\n", tr.module.path, tr.resolvedVersion)
	// Create a pristine copy of the git root with no history
	td, err := mt.tempDir("workdir")
	if err != nil {
		return fmt.Errorf("failed to create workdir root: %v", err)
	}
	for _, s := range m.scripts {
		// uses the pattern of directory construction from testscript
		name := strings.TrimSuffix(filepath.Base(s), ".txt")
		dir := filepath.Join(td, "script-"+name, repoDir)
		if err := os.MkdirAll(dir, 0777); err != nil {
			return fmt.Errorf("failed to create workdir for %s: %v", s, err)
		}
		if _, err = gitDir(m.gitRoot, "worktree", "add", "--detach", dir); err != nil {
			return fmt.Errorf("failed to create copy of current HEAD from %s: %v", m.gitRoot, err)
		}
		defer gitDir(m.gitRoot, "worktree", "remove", dir)
		if m.hasStaged && mt.staged {
			// TODO make this more efficient by not reading into memory
			var changes, stderr bytes.Buffer
			read := exec.Command("git", "diff", "--staged")
			read.Dir = m.gitRoot
			read.Stdout = &changes
			read.Stderr = &stderr
			if err := read.Run(); err != nil {
				return fmt.Errorf("failed to read staged changes in %s via [%v]: %v\n%s", m.gitRoot, read, err, stderr.Bytes())
			}
			write := exec.Command("git", "apply")
			write.Dir = dir
			write.Stdin = &changes
			if out, err := write.CombinedOutput(); err != nil {
				return fmt.Errorf("failed to apply staged changes in %s via [%v]: %v\n%s", td, write, err, out)
			}
		}
	}

	rmi := runModuleInfo{
		self:          mt.self,
		manifestDir:   m.manifestDir,
		workdirRoot:   td,
		relPath:       m.relPath,
		testerRelPath: m.testerRelPath,
		cuePath:       cuePath,
		version:       version,
		update:        allowUpdate && mt.update,
		verbose:       mt.verbose,
	}

	start := time.Now()
	defer func() {
		tr.duration = time.Since(start)
	}()
	if mt.unsafe {
		return runModule(tr.log, rmi)
	}
	return dockerRunModule(mt.image, tr.log, rmi)
}

type runModuleInfo struct {
	self          string
	manifestDir   string
	workdirRoot   string
	relPath       string
	testerRelPath string
	cuePath       string
	version       string
	update        bool
	verbose       bool
}

func runModule(log io.Writer, info runModuleInfo) (err error) {
	params := testscript.Params{
		UpdateScripts: info.update,
		Dir:           info.manifestDir,
		WorkdirRoot:   info.workdirRoot,
		Setup: func(e *testscript.Env) error {
			// Limit concurrency across all testscript runs
			// e.Defer(m.tester.limit())

			// Ensure that cue is on the PATH
			newPath := filepath.Dir(info.cuePath) + string(os.PathListSeparator) + e.Getenv("PATH")
			e.Setenv("PATH", newPath)

			// Set the working directory to be module
			e.Cd = filepath.Join(e.WorkDir, repoDir, info.relPath)
			return nil
		},
		Cmds: map[string]func(ts *testscript.TestScript, neg bool, args []string){
			"cue": buildCmdCUE(info.cuePath),
		},
	}
	// TODO: improve logging/printing/errors when we make things concurrent
	r := newRunT("", nil, info.verbose)
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
		var context []string
		if info.testerRelPath != "" {
			context = append(context, info.testerRelPath)
		}
		context = append(context, c.name, info.version)
		if !c.failed && !c.verbose {
			continue
		}
		passFail := "PASS"
		if c.failed {
			passFail = "FAIL"
		}
		fmt.Fprintf(log, "--- %s: %s\n%s", passFail, path.Join(context...), indent(c.log, "\t"))
	}
	if r.failed {
		return errTestFail
	}
	return nil
}

func dockerRunModule(image string, log io.Writer, info runModuleInfo) (err error) {
	// TODO we could add support for limiting the concurrency of testscript
	// tests in the child process via something like:
	//
	// https://go2goplay.golang.org/p/YZxV9iVWDqf
	args := []string{
		"docker", "run", "--rm", "-t",

		// All docker images used by unity must support this interface
		"-e", fmt.Sprintf("USER_UID=%v", os.Geteuid()),
		"-e", fmt.Sprintf("USER_GID=%v", os.Getegid()),

		// Add mounts
		"-v", info.manifestDir + ":/unity/manifestDir",
		"-v", info.workdirRoot + ":/unity/workdirRoot",
		"-v", info.cuePath + ":/unity/cue",
		"-v", info.self + ":/unity/unity",

		image,

		"/unity/unity", "docker",
		"--manifest", "/unity/manifestDir",
		"--workdirRoot", "/unity/workdirRoot",
		"--relPath", info.relPath,
		"--testerRelPath", info.testerRelPath,
		"--cuePath", "/unity/cue",
		"--version", info.version,
	}
	if info.update {
		args = append(args, "--update")
	}
	if info.verbose {
		args = append(args, "--verbose")
	}
	// TODO remove the multi-writer
	var buf bytes.Buffer
	comb := io.MultiWriter(&buf, log)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = comb
	cmd.Stderr = comb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to run [%v]: %v\n%s", cmd, err, buf.Bytes())
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

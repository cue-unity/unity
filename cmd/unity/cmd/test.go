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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"github.com/spf13/cobra"

	// imported for side effect of setting
	// internal/unityembed exported var
	_ "github.com/cue-unity/unity"
	"github.com/cue-unity/unity/internal/unityembed"
)

const (
	flagTestUpdate      flagName = "update"
	flagTestCorpus      flagName = "corpus"
	flagTestRun         flagName = "run"
	flagTestDir         flagName = "dir"
	flagTestVerbose     flagName = "verbose"
	flagTestNoPath      flagName = "nopath"
	flagTestOverlay     flagName = "overlay"
	flagTestUnsafe      flagName = "unsafe"
	flagTestStaged      flagName = "staged"
	flagTestIgnoreDirty flagName = "ignore-dirty"
	flagTestSelf        flagName = "self"
	flagTestSkipBase    flagName = "skip-base"

	// dockerImage is the image we use when running in safe mode
	//
	// TODO: add support for custom docker images. Such images must support the interface
	// of requiring USER_UID and USER_GID
	dockerImage = "docker.io/cueckoo/unity@sha256:e9480dcb2a99ea7a128c0d560964aa4d1f642485da1328b1daa2e46800e33b59"
)

// newTestCmd creates a new test command
//
// TODO: update the command's long description
func newTestCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test",
		Short: "test the CUE corpus",
		Long: `
Need to document this command
`,
		RunE: mkRunE(c, testDef),
	}
	cmd.Flags().Bool(string(flagTestUpdate), false, "update files within test archives when cmp fails")
	cmd.Flags().Bool(string(flagTestCorpus), false, "run tests for the submodules of the git repository that contains the working directory.")
	cmd.Flags().String(string(flagTestRun), ".", "run only those tests matching the regular expression.")
	cmd.Flags().StringP(string(flagTestDir), "d", ".", "search path for the project or corpus")
	cmd.Flags().BoolP(string(flagTestVerbose), "v", false, "verbose output; log all script runs")
	cmd.Flags().Bool(string(flagTestNoPath), false, "do not allow CUE version PATH. Useful for CI")
	cmd.Flags().String(string(flagTestOverlay), "", "the directory from which to source overlays")
	cmd.Flags().Bool(string(flagTestUnsafe), os.Getenv("UNITY_UNSAFE") != "", "do not use Docker for executing scripts")
	cmd.Flags().Bool(string(flagTestStaged), false, "apply staged changes during tests")
	cmd.Flags().Bool(string(flagTestIgnoreDirty), false, "ignore untracked files, and staged files unless --staged")
	cmd.Flags().String(string(flagTestSelf), os.Getenv("UNITY_SELF"), "the context within which we can resolve self to build for docker")
	cmd.Flags().Bool(string(flagTestSkipBase), false, "do not test base versions")

	return cmd
}

func testDef(c *Command, args []string) error {
	debug := flagDebug.Bool(c)

	ctx := cuecontext.New()

	// dir is the context within which we will be running
	dir := flagTestDir.String(c)

	// selfDir is used in the case that:
	//
	// * we are running in safe mode, i.e. scrip tests are run in docker,
	// * the running binary (i.e. unity, self) does not match in terms of
	// GOOS/GOARCH the target docker image
	// * the running binary's main module Version is not a semver version
	// (because otherwise we would be able to resolve everything through the
	// proxy)
	//
	// In case --self is not provided, then we fallback to dir. Either way
	// the in this scenario it must be possible to resolve the unity module
	// from within selfDir
	selfDir := flagTestSelf.String(c)
	if selfDir == "" {
		selfDir = dir
	}

	// Find the git root
	gitRoot, err := gitDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("failed to determine git root: %v", err)
	}
	gitRoot = strings.TrimSpace(gitRoot)

	manifestDef := loadManifestSchema(ctx)
	if err := manifestDef.Err(); err != nil {
		return fmt.Errorf("failed to load #Manifest definition: %v", err)
	}

	// Verify that the overlay directory, if provided, exists
	overlayDir := flagTestOverlay.String(c)
	if overlayDir != "" {
		fi, err := os.Stat(overlayDir)
		if err != nil {
			return fmt.Errorf("failed to find overlay directory %s: %v", overlayDir, err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("overlay directory %s is not a directory", overlayDir)
		}
		abs, err := filepath.Abs(overlayDir)
		if err != nil {
			return fmt.Errorf("failed to make path %s absolute: %v", overlayDir, err)
		}
		overlayDir = abs
	}

	bh, err := newBuildHelper()
	if err != nil {
		return fmt.Errorf("failed to create build helper: %v", err)
	}
	defer bh.cache.Trim()

	var self string
	if !flagTestUnsafe.Bool(c) {
		if err := bh.targetDocker(dockerImage); err != nil {
			return fmt.Errorf("failed inspect docker image %s: %v", dockerImage, err)
		}
		// Work out whether the current GOOS/GOARCH is appropriate for the target
		// docker image
		td, err := ioutil.TempDir("", "unity-self-dir")
		if err != nil {
			return fmt.Errorf("failed to create a temp directory for self build: %v", err)
		}
		defer os.RemoveAll(td)
		self, err = bh.pathToSelf(selfDir, td, false)
		if err != nil {
			return fmt.Errorf("failed to derive path to self: %v", err)
		}
	}

	// Note: we can't pre-resolve any versions here because that needs to happen
	// in the context of a project for go.mod versions (at least)
	vr, err := newVersionResolver(resolverConfig{
		bh:        bh,
		allowPATH: !flagTestNoPath.Bool(c),
		debug:     debug,
	})
	if err != nil {
		return fmt.Errorf("could not create version resolver: %v", err)
	}

	// Perform some basic validation on the --update flag.
	if len(args) > 1 && flagTestUpdate.Bool(c) {
		return fmt.Errorf("cannot supply --update and multiple versions")
	}

	// Perform some basic validation on the --skip-base flag.
	if len(args) == 0 && flagTestSkipBase.Bool(c) {
		return fmt.Errorf("nothing to test")
	}

	mt, err := newModuleTester(moduleTester{
		self:            self, // only used in safe mode
		buildHelper:     bh,
		image:           dockerImage,
		gitRoot:         gitRoot,
		overlayDir:      overlayDir,
		versionResolver: vr,
		runtime:         ctx,
		manifestDef:     manifestDef,
		unsafe:          flagTestUnsafe.Bool(c),
		update:          flagTestUpdate.Bool(c),
		staged:          flagTestStaged.Bool(c),
		ignoreDirty:     flagTestIgnoreDirty.Bool(c),
		verbose:         flagTestVerbose.Bool(c),
		skipBase:        flagTestSkipBase.Bool(c),
	})
	defer mt.cleanup()

	if err != nil {
		return fmt.Errorf("failed to create module tester: %v", err)
	}

	if flagTestCorpus.Bool(c) {
		return testCorpus(c, mt, args)
	}
	err = testProject(c, mt, args)
	if errors.Is(err, errTestFail) {
		// we will have printed everything we need to
		exit()
	}
	return err
}

func loadManifestSchema(ctx *cue.Context) cue.Value {
	overlay := make(map[string]load.Source)
	fs.WalkDir(unityembed.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			panic(err) // this is seriously bad
		}
		if !d.Type().IsRegular() {
			return nil
		}
		contents, err := unityembed.FS.ReadFile(path)
		if err != nil {
			panic(err) // this is seriously bad
		}
		overlay[filepath.Join("/", path)] = load.FromBytes(contents)
		return nil
	})
	conf := &load.Config{
		Dir:     "/",
		Overlay: overlay,
	}
	bps := load.Instances([]string{"."}, conf)
	return ctx.BuildInstance(bps[0]).LookupPath(cue.MakePath(cue.Def("Manifest")))

}

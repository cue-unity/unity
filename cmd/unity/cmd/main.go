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

// Package cmd is the implementation behind unity
package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"

	"cuelang.org/go/cue/errors"
	"github.com/rogpeppe/go-internal/cache"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"
)

const (
	// moduleSelf is used to identify build info for the running unity
	// process
	moduleSelf = "github.com/cue-sh/unity"

	// mainSelf is the package path of unity the main package
	mainSelf = moduleSelf + "/cmd/unity"

	flagDebug flagName = "debug"
)

// Main runs the unity tool and returns the code for passing to os.Exit.
//
// We follow the same approach here as the cue command (as well as using the
// using the same version of Cobra) for consistency. Panic is used as a strategy
// for early-return from any running command.
func Main() int {
	cwd, _ := os.Getwd()
	err := mainErr(context.Background(), os.Args[1:])
	if err != nil {
		if err != errPrintedError {
			errors.Print(os.Stderr, err, &errors.Config{
				Cwd: cwd,
			})
		}
		return 1
	}
	return 0
}

func mainErr(ctx context.Context, args []string) (err error) {
	defer recoverError(&err)
	cmd, err := New(args)
	if err != nil {
		return err
	}
	return cmd.Run(ctx)
}

func New(args []string) (cmd *Command, err error) {
	defer recoverError(&err)

	cmd = newRootCmd()
	rootCmd := cmd.root
	if len(args) == 0 {
		return cmd, nil
	}
	rootCmd.SetArgs(args)
	return
}

func newRootCmd() *Command {
	cmd := &cobra.Command{
		Use:   "unity",
		Short: "unity automates the process of different versions of CUE against a corpus of CUE code.",
		Long: `Details to follow...
`,
		SilenceUsage: true,
	}

	c := &Command{Command: cmd, root: cmd}

	cmd.PersistentFlags().Bool(string(flagDebug), os.Getenv("UNITY_DEBUG") != "", "debug output")

	subCommands := []*cobra.Command{
		newTestCmd(c),
		newDockerCmd(c),
	}
	// TODO: add help topics

	for _, sub := range subCommands {
		cmd.AddCommand(sub)
	}

	return c
}

// pathToSelf returns the directory within which a compiled version of self
// called "unity" appropriate for running within a docker container exists.
// temp indicates to the caller whether that is in a temporary location that
// must be purged after use. dir is the context within which we are running
// and can find self
func pathToSelf(dir string) (self string, temp bool, err error) {
	// When running tests, specifically testscript tests, we will not
	// be running in the context of this module. So we override the directory
	// for the resolution of self. Feels a bit gross, but reasonable given
	// it should only be the unity tests where this is necessary
	if v := os.Getenv("UNITY_TEST_PATH_TO_SELF"); v != "" {
		return v, false, nil
	}

	// compileTarget indicates where we should ensure self is built
	var compileTarget string

	// vcache and cacheHash will be set when we should write self back to a cache
	var vcache *cache.Cache
	var cacheHash *cache.Hash

	// Work out where we need to compile self.
	bi, ok := debug.ReadBuildInfo()
	if !ok || bi.Main.Version == "(devel)" {
		// Assert that we are running in the context of the module that
		// provides the main package. i.e. that the main module is
		// github.com/cue-sh/unity. This is not guaranteed to be the case
		// but for the purposes of what we need this is probably sufficient.
		// If it isn't, then we cross that bridge when we come to it
		list := exec.Command("go", "list", "-m", "-f={{.Dir}}", moduleSelf)
		list.Dir = dir
		listOut, err := list.CombinedOutput()
		if err != nil {
			return "", false, fmt.Errorf("failed to determine context for %s; in %s ran [%v]: %v\n%s", moduleSelf, dir, list, err, listOut)
		}
		compileTarget = strings.TrimSpace(string(listOut))
		compileTarget = filepath.Join(compileTarget, ".bin")
	} else {
		m := bi.Main
		if bi.Main.Replace != nil {
			m = *bi.Main.Replace
		}
		if filepath.IsAbs(mainSelf) {
			compileTarget = m.Path
		} else {
			// Assert that we have an actual semver version
			if !semver.IsValid(m.Version) {
				return "", false, fmt.Errorf("found invalid semver version %q for %s", m.Version, moduleSelf)
			}
			// Whether we have a cache hit or miss we will use a temp directory
			compileTarget, err = ioutil.TempDir("", "unity-self")
			if err != nil {
				return "", false, fmt.Errorf("failed to create temp directory for self: %v", err)
			}
			ucd, err := os.UserCacheDir()
			if err != nil {
				return "", false, fmt.Errorf("failed to determine user cache dir: %v", err)
			}
			binCache := filepath.Join(ucd, "unity", "bin")
			if err := os.MkdirAll(binCache, 0777); err != nil {
				return "", false, fmt.Errorf("failed to ensure %s exists: %v", binCache, err)
			}
			vcache, err = cache.Open(binCache)
			if err != nil {
				return "", false, fmt.Errorf("failed to open cache at %s: %v", binCache, err)
			}
			defer vcache.Trim()
			cacheHash = cache.NewHash("version")
			cacheHash.Write([]byte(m.Version))
			if contents, _, err := vcache.GetBytes(cacheHash.Sum()); err == nil {
				// cache hit
				target := filepath.Join(compileTarget, "unity")
				if err := os.WriteFile(target, contents, 0777); err != nil {
					return "", false, fmt.Errorf("failed to write self to %s: %v", target, err)
				}
				return compileTarget, true, nil
			}
		}
	}
	target := filepath.Join(compileTarget, "unity")
	build := exec.Command("go", "build", "-o", target, mainSelf)
	build.Dir = dir
	build.Env = append(os.Environ(),
		"GOOS=linux",
		"GOARCH=amd64",
		"CGO_ENABLED=0",
	)
	buildOut, err := build.CombinedOutput()
	if err != nil {
		return "", false, fmt.Errorf("failed to build %s; ran [%v]: %v\n%s", mainSelf, build, err, buildOut)
	}
	if vcache == nil {
		return compileTarget, false, nil
	}
	targetFile, err := os.Open(target)
	if err != nil {
		return "", false, fmt.Errorf("failed to open compiled version of self")
	}
	// Write back to the cache
	if _, _, err := vcache.Put(cacheHash.Sum(), targetFile); err != nil {
		return "", false, fmt.Errorf("failed to write compiled version of self to the cache: %v", err)
	}
	return compileTarget, true, nil
}

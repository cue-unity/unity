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
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"github.com/cue-sh/unity"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
)

const (
	flagUpdate  flagName = "update"
	flagCorpus  flagName = "corpus"
	flagRun     flagName = "run"
	flagDir     flagName = "dir"
	flagVerbose flagName = "verbose"
	flagNoPath  flagName = "nopath"
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
	cmd.Flags().Bool(string(flagUpdate), false, "update files within tests when a cmp fails")
	cmd.Flags().Bool(string(flagCorpus), false, "run tests for the submodules of the git repository that contains the working directory.")
	cmd.Flags().String(string(flagRun), ".", "run only those tests matching the regular expression.")
	cmd.Flags().StringP(string(flagDir), "d", ".", "search path for the project or corpus")
	cmd.Flags().BoolP(string(flagVerbose), "v", false, "verbose output; log all script runs")
	cmd.Flags().Bool(string(flagNoPath), false, "do not allow CUE version PATH. Useful for CI")
	return cmd
}

func testDef(c *Command, args []string) error {
	debug := flagDebug.Bool(c)

	vr, err := newVersionResolver(!flagNoPath.Bool(c))
	vr.debug = debug
	if err != nil {
		return fmt.Errorf("could not create version resolver: %v", err)
	}
	var eg errgroup.Group
	for _, v := range args {
		v := v
		eg.Go(func() error {
			_, err := vr.resolve(v)
			return err
		})
	}
	if err := eg.Wait(); err != nil {
		return fmt.Errorf("failed to pre-resolve versions %v: %v", args, err)
	}

	var r cue.Runtime
	dir := flagDir.String(c)

	// Find the git root
	gitRoot, err := gitDir(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("failed to determine git root: %v", err)
	}
	gitRoot = strings.TrimSpace(gitRoot)

	// Load the #Tests definition
	insts, err := r.Unmarshal(unity.InstanceData)
	if err != nil {
		return fmt.Errorf("failed to load embedded unity instance: %v", err)
	}
	manifestDef := insts[0].LookupDef("#Manifest")
	if err := manifestDef.Err(); err != nil {
		return fmt.Errorf("failed to resolve #Manifest definition: %v", err)
	}

	mt := newModuleTester(vr, &r, manifestDef)
	mt.verbose = flagVerbose.Bool(c)

	if flagCorpus.Bool(c) {
		return testCorpus(c, mt, gitRoot, args)
	}
	err = testProject(c, mt, gitRoot, args)
	if errors.Is(err, errTestFail) {
		// we will have printed everything we need to
		exit()
	}
	return err
}

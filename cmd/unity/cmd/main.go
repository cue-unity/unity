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
	"os"

	"cuelang.org/go/cue/errors"
	"github.com/spf13/cobra"
)

const (
	// cueModule is the path used to identify the module that contains
	// cmd/cue
	cueModule = "cuelang.org/go"

	// cmdCue is the import path to cmd/cue
	cmdCue = cueModule + "/cmd/cue"

	// moduleSelf is used to identify build info for the running unity
	// process
	moduleSelf = "github.com/cue-unity/unity"

	// mainSelf is the package path of unity the main package
	mainSelf = moduleSelf + "/cmd/unity"

	mainName = "unity"

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

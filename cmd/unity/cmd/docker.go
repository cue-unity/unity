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
	"os"

	"github.com/spf13/cobra"
)

const (
	flagDockerSelf          flagName = "self"
	flagDockerManifest      flagName = "manifest"
	flagDockerGitRoot       flagName = "gitRoot"
	flagDockerRelPath       flagName = "relPath"
	flagDockerTesterRelPath flagName = "testerRelPath"
	flagDockerCUEPath       flagName = "cuePath"
	flagDockerVersion       flagName = "version"
	flagDockerUpdate        flagName = "update"
	flagDockerVerbose       flagName = "verbose"
)

// newTestCmd creates a new test command
//
// TODO: update the command's long description
func newDockerCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "docker",
		Hidden: true,
		RunE:   mkRunE(c, dockerDef),
	}
	cmd.Flags().String(string(flagDockerSelf), "", "the path to self")
	cmd.Flags().String(string(flagDockerManifest), "", "the path to the manifest directory for the project")
	cmd.Flags().String(string(flagDockerGitRoot), "", "the git root of the project under test")
	cmd.Flags().String(string(flagDockerRelPath), "", "the relative path to the project git root")
	cmd.Flags().String(string(flagDockerTesterRelPath), "", "the relative path the module tester git root")
	cmd.Flags().String(string(flagDockerCUEPath), "", "the path to the CUE binary to use")
	cmd.Flags().String(string(flagDockerVersion), "", "the version being tested")
	cmd.Flags().Bool(string(flagDockerUpdate), false, "update test archives when cmp fails")
	cmd.Flags().Bool(string(flagDockerVerbose), false, "run in verbose mode")
	return cmd
}

func dockerDef(c *Command, args []string) error {
	return runModule(os.Stdout,
		runModuleInfo{
			self:          flagDockerSelf.String(c),
			manifestDir:   flagDockerManifest.String(c),
			gitRoot:       flagDockerGitRoot.String(c),
			relPath:       flagDockerRelPath.String(c),
			testerRelPath: flagDockerTesterRelPath.String(c),
			cuePath:       flagDockerCUEPath.String(c),
			version:       flagDockerVersion.String(c),
			update:        flagDockerUpdate.Bool(c),
			verbose:       flagDockerVerbose.Bool(c),
		},
	)
}

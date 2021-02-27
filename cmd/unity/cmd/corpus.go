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
	"os"
	"path/filepath"
	"strings"
)

func testCorpus(cmd *Command, mt *moduleTester, versions []string) error {
	mt.logger = func(m *module, version string) error {
		rel, err := filepath.Rel(mt.cwd, m.root)
		if err != nil {
			return fmt.Errorf("failed to determine %s relative to %s: %v", m.root, mt.cwd, err)
		}
		fmt.Fprintf(os.Stderr, "testing %s against version %s\n", rel, version)
		return nil
	}
	submodConfig := filepath.Join(mt.gitRoot, ".gitmodules")
	if _, err := os.Stat(submodConfig); err != nil {
		return fmt.Errorf("failed to find git submodules config file at %s: %v", submodConfig, err)
	}

	submods, err := gitDir(mt.gitRoot, "config", "--file", ".gitmodules", "--get-regexp", "path")
	if err != nil {
		return fmt.Errorf("failed to list git submodules via in %s: %v", mt.gitRoot, err)
	}

	var modules []*module

	// Format of submods will be one submodule per line, where the path of the submodule relative
	// to the git root is the second field
	for _, line := range strings.Split(submods, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Check that the submodule exists locally first, using the existence of .git as that sign
		fields := strings.Fields(line)
		projPath := filepath.Join(mt.gitRoot, fields[1])
		if _, err := os.Stat(filepath.Join(projPath, ".git")); err != nil {
			continue
		}
		ms, err := mt.deriveModules(projPath)
		if err != nil {
			return fmt.Errorf("failed to derive modules under %s: %v", projPath, err)
		}
		if len(ms) == 0 {
			return fmt.Errorf("could not find any CUE module roots under %s", projPath)
		}
		modules = append(modules, ms...)
	}

	return mt.test(modules, versions)
}

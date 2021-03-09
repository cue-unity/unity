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
)

const (
	commitVersionPrefix = "commit:"
)

// commitResolver resolves a "commit:$hash" version to a commit from the master
// branch of the CUE repository. The result is stored in the unity user cache
// directory.
type commitResolver struct {
	cc *commonCUEResolver
}

func newCommitResolver(c resolverConfig) (resolver, error) {
	res := &commitResolver{
		cc: c.commonCUEResolver,
	}
	return res, nil
}

func (g *commitResolver) resolve(version, _, _, target string) (string, error) {
	if !strings.HasPrefix(version, commitVersionPrefix) {
		return "", errNoMatch
	}
	version = strings.TrimPrefix(version, commitVersionPrefix)
	return g.cc.resolve(version, target, func(c *commonCUEResolver) (string, error) {
		if _, err := gitDir(c.dir, "checkout", version); err != nil {
			if _, err := gitDir(c.dir, "fetch", "origin"); err != nil {
				return "", fmt.Errorf("failed to fetch origin: %v", err)
			}
		}
		if _, err := gitDir(c.dir, "checkout", version); err != nil {
			return "", fmt.Errorf("failed to checkout %s: %v", version, err)
		}
		version, err := gitDir(c.dir, "rev-parse", "HEAD")
		if err != nil {
			return "", fmt.Errorf("failed to rev-parse HEAD: %v", err)
		}
		version = strings.TrimSpace(version)
		return version, nil
	})
}

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

type gerritRefResolver struct {
	cc *commonCUEResolver
}

// commitResolver resolves a "refs/changes/..." CL patchset reference to a
// change from the CUE Gerrit server. The result is stored in the unity user
// cache directory.
func newGerritRefResolver(c resolverConfig) (resolver, error) {
	res := &gerritRefResolver{
		cc: c.commonCUEResolver,
	}
	return res, nil
}

func (g *gerritRefResolver) resolve(version, _, _, target string) error {
	if !strings.HasPrefix(version, "refs/changes/") {
		return errNoMatch
	}
	return g.cc.resolve(version, target, func(c *commonCUEResolver) (string, error) {
		// fetch the version
		if _, err := gitDir(c.dir, "fetch", cueGitSource, version); err != nil {
			return "", fmt.Errorf("failed to fetch %s: %v", version, err)
		}
		// move to FETCH_HEAD
		if _, err := gitDir(c.dir, "checkout", "FETCH_HEAD"); err != nil {
			return "", fmt.Errorf("failed to checkout FETCH_HEAD: %v", err)
		}
		return version, nil
	})
}

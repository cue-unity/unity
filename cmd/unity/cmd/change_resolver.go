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

	"github.com/andygrunwald/go-gerrit"
)

const (
	changeVersionPrefix = "change:"
)

type changeResolver struct {
	cc *commonCUEResolver
}

// changeResolver resolves a "change:$changeID/$revisionID" reference to a
// change from the CUE Gerrit server. The result is stored in the unity user
// cache directory.
func newChangeResolver(c resolverConfig) (resolver, error) {
	res := &changeResolver{
		cc: c.commonCUEResolver,
	}
	return res, nil
}

func (g *changeResolver) resolve(version, _, _, target string) (string, error) {
	if !strings.HasPrefix(version, changeVersionPrefix) {
		return "", errNoMatch
	}
	changeID, revisionID, found := strings.Cut(strings.TrimPrefix(version, changeVersionPrefix), "/")
	if !found {
		return "", errNoMatch
	}
	return g.cc.resolve(version, target, func(c *commonCUEResolver) (string, error) {
		client, err := gerrit.NewClient("https://review.gerrithub.io", nil)
		if err != nil {
			return "", fmt.Errorf("failed to create Gerrit client: %v", err)
		}
		change, _, err := client.Changes.GetChange(changeID, &gerrit.ChangeOptions{
			AdditionalFields: []string{"ALL_REVISIONS"},
		})
		if err != nil {
			return "", fmt.Errorf("failed to resolve revisions for %s: %v", changeID, err)
		}
		revision, ok := change.Revisions[revisionID]
		if !ok {
			return "", fmt.Errorf("failed to resolve revision %s/%s", changeID, revisionID)
		}

		// fetch the version
		if _, err := gitDir(c.dir, "fetch", cueGitSource, revision.Ref); err != nil {
			return "", fmt.Errorf("failed to fetch %s: %v", version, err)
		}
		// move to FETCH_HEAD
		if _, err := gitDir(c.dir, "switch", "-d", "FETCH_HEAD"); err != nil {
			return "", fmt.Errorf("failed to checkout FETCH_HEAD: %v", err)
		}
		return version, nil
	})
}

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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/cue-sh/unity/internal/copy"
)

var errNoMatch = errors.New("resolver not appropriate for version")

type versionResolver struct {
	// resolvers are the list of resolver implementations we support
	resolvers []resolver
}

type resolver interface {
	// resolve derives version in the context of dir
	// (some versions are context-dependent, e.g. go.mod).
	resolve(version, dir, working, targetDir string) error
}

func newVersionResolver(c resolverConfig) (*versionResolver, error) {
	inits := []func(resolverConfig) (resolver, error){
		newPathResolver,
		newSemverResolver,
		newAbsolutePathResolver,
		newGerritRefResolver,
	}
	var resolvers []resolver
	for i, rb := range inits {
		r, err := rb(c)
		if err != nil {
			return nil, fmt.Errorf("failed to build resolver %v: %v", i, err)
		}
		resolvers = append(resolvers, r)
	}
	res := &versionResolver{
		resolvers: resolvers,
	}
	return res, nil
}

func (vr *versionResolver) resolve(version, dir, working, targetDir string) error {
	var errs []error
	var match int
	for _, r := range vr.resolvers {
		err := r.resolve(version, dir, working, targetDir)
		switch err {
		case nil:
			match++
		case errNoMatch:
		default:
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		var buf bytes.Buffer
		join := ""
		for _, e := range errs {
			fmt.Fprintf(&buf, "%v%v", join, e)
			join = "\n"
		}
		return fmt.Errorf("got errors during version resolution:\n%s", buf.Bytes())
	}
	if match != 1 {
		return fmt.Errorf("expected 1 match; got %v", match)
	}
	return nil
}

type resolverConfig struct {
	bh        *buildHelper
	allowPATH bool
	debug     bool
}

// debugf logs useful information about version resolution to stderr
// in case debugging is enabled
func (rs resolverConfig) debugf(format string, args ...interface{}) {
	if rs.debug {
		out := fmt.Sprintf(format, args...)
		if out != "" && out[len(out)-1] != '\n' {
			out += "\n"
		}
		fmt.Fprint(os.Stderr, out)
	}
}

func copyExecutableFile(src, dst string) error {
	if err := copy.File(src, dst); err != nil {
		return fmt.Errorf("failed to copy %s to %s: %v", src, dst, err)
	}
	dir := filepath.Dir(dst)
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to get directory information for %s: %v", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("somehow %s is not a dir", dir)
	}
	perm := fi.Mode().Perm()
	if err := os.Chmod(dst, perm); err != nil {
		return fmt.Errorf("failed to set permissions of %s to %v: %v", dst, perm, err)
	}
	return nil
}

var (
	errPATHNotAllowed = errors.New("CUE version of PATH not permitted")
)

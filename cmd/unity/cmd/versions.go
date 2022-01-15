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

	"github.com/cue-unity/unity/internal/copy"
)

var errNoMatch = errors.New("resolver not appropriate for version")

type versionResolver struct {
	// resolvers are the list of resolver implementations we support
	resolvers []resolver
}

type resolver interface {
	// resolve derives version in the context of dir, copying the relevant
	// binary to target. working can be used as a temporary working directory.
	resolve(version, dir, working, target string) (string, error)
}

func newVersionResolver(c resolverConfig) (*versionResolver, error) {
	cc, err := newCommonCUEREsolver(c)
	if err != nil {
		return nil, fmt.Errorf("failed to create common CUE resolver: %v", err)
	}
	cp, err := newCommonPathResolver(c)
	if err != nil {
		return nil, fmt.Errorf("failed to create common path resolver: %v", err)
	}
	c.commonCUEResolver = cc
	c.commonPathResolver = cp
	inits := []func(resolverConfig) (resolver, error){
		newPathResolver,
		newSemverResolver,
		newAbsolutePathResolver,
		newGerritRefResolver,
		newCommitResolver,
		newGoModResolver,
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

func (vr *versionResolver) resolve(version, dir, working, target string) (string, error) {
	var errs []error
	var versions []string
	for _, r := range vr.resolvers {
		v, err := r.resolve(version, dir, working, target)
		switch err {
		case nil:
			versions = append(versions, v)
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
		return "", fmt.Errorf("got errors during version resolution:\n%s", buf.Bytes())
	}
	if l := len(versions); l != 1 {
		return "", fmt.Errorf("expected 1 match; got %v", l)
	}
	return versions[0], nil
}

type resolverConfig struct {
	bh                 *buildHelper
	allowPATH          bool
	debug              bool
	commonCUEResolver  *commonCUEResolver
	commonPathResolver *commonPathResolver
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

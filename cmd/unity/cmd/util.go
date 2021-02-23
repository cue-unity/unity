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
	"fmt"
	"os"
	"os/exec"
	"strings"

	"cuelang.org/go/cue/errors"
	"github.com/rogpeppe/go-internal/testscript"
)

func gitDir(dir string, args ...string) (string, error) {
	return gitEnvDir(os.Environ(), dir, args...)
}

func gitEnvDir(env []string, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Env = env
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %v\n%s%s", strings.Join(args, " "), err, stderr.Bytes(), out)
	}
	return string(out), nil
}

// runT implements testscript.T and is used in the call to testscript.Run
//
// This is basically a poor man's testing.T. Fully flesh this out in order
// to have parallel runs etc
type runT struct {
	name     string
	parent   *runT
	children []*runT
	verbose  bool
	log      *bytes.Buffer
	failed   bool
}

func newRunT(name string, parent *runT) *runT {
	return &runT{
		name:   name,
		parent: parent,
		log:    new(bytes.Buffer),
	}
}

var _ testscript.T = (*runT)(nil)

func (r *runT) Skip(is ...interface{}) {
	panic(skipRun)
}

func (r *runT) Fatal(is ...interface{}) {
	r.Log(is...)
	r.FailNow()
}

func (r *runT) Parallel() {
}

func (r *runT) Log(is ...interface{}) {
	fmt.Fprint(r.log, is...)
}

func (r *runT) FailNow() {
	r.failed = true
	panic(failedRun)
}

func (r *runT) Run(n string, f func(t testscript.T)) {
	child := newRunT(n, r)
	r.children = append(r.children, child)
	defer func() {
		switch err := recover(); err {
		case nil, skipRun, failedRun:
			// Normal operation
			r.failed = r.failed || child.failed
		default:
			panic(err)
		}
	}()
	f(child)
}

func (r *runT) Verbose() bool {
	return r.verbose
}

var (
	failedRun = errors.New("failed run")
	skipRun   = errors.New("skip")
)

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
	"path/filepath"
	"runtime/debug"
	"testing"

	"github.com/rogpeppe/go-internal/goproxytest"
)

func TestBuildInfoBuild(t *testing.T) {
	bi := &debug.BuildInfo{
		Path: "example.com/blah",
		Main: debug.Module{
			Path:    "example.com/blah",
			Version: "v1.0.0",
			Sum:     "h1:37CzVIxObPh1ahpBsj2X47rua0Z856FTQsC2WRZk3co=",
			Replace: (*debug.Module)(nil),
		},
	}

	mods, ok := buildInfoToModules(bi, true)
	if !ok {
		t.Fatalf("expected modules to be ok")
	}
	td := t.TempDir()
	bh, err := newBuildHelper()
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	modDir := filepath.Join(cwd, "testdata", "mod")
	srv, err := goproxytest.NewServer(modDir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	t.Setenv("GOPROXY", srv.URL)
	t.Setenv("GONOSUMDB", "*")
	if err := bh.writeGoModSum(td, "example.com/blah", mods); err != nil {
		t.Fatal(err)
	}
	// get := exec.Command("go", "get", "example.com/blah")
	// get.Dir = td
	// if out, err := get.CombinedOutput(); err != nil {
	// 	t.Fatal(fmt.Errorf("failed to run %v: %v\n%s", get, err, out))
	// }
	// gosum, err := os.ReadFile(filepath.Join(td, "go.sum"))
	// if err != nil {
	// 	t.Fatal(err)
	// }
	// fmt.Printf("go.sum:\n%s", gosum)
	targt := filepath.Join(td, "blah")
	if err := bh.buildAndCache(td, targt, "example.com/blah"); err != nil {
		t.Fatal(err)
	}
}

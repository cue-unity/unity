package cmd

import (
	"io/ioutil"
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
	td, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(td)
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
	defer srv.Close()
	pushEnv := func(k, v string) func() {
		curr := os.Getenv(k)
		if err := os.Setenv(k, v); err != nil {
			t.Fatal(err)
		}
		return func() {
			os.Setenv(k, curr)
		}
	}
	defer pushEnv("GOPROXY", srv.URL)()
	defer pushEnv("GONOSUMDB", "*")()
	if err := bh.writeGoModSum(td, "example.com/blah", mods); err != nil {
		t.Fatal(err)
	}
	// get := exec.Command("go", "get", "-d", "example.com/blah")
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

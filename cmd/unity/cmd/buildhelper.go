package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/rogpeppe/go-internal/cache"
	"golang.org/x/mod/semver"
)

type buildHelper struct {
	// cache is the build artefact cache we use to speed up the use of cue/unity
	// binaries
	cache *cache.Cache

	// targetGOOS is the GOOS required by the target docker image
	targetGOOS string

	// targetGOARCH is the GOARCH required by the target docker image
	targetGOARCH string
}

// newBuildHelper creates a new build helper.
func newBuildHelper() (*buildHelper, error) {
	vcache, err := openUnityCache()
	if err != nil {
		return nil, err
	}
	res := &buildHelper{
		cache:        vcache,
		targetGOOS:   runtime.GOOS,
		targetGOARCH: runtime.GOARCH,
	}
	return res, nil
}

// targetDocker updates bh to target the supplied docker image. The docker
// image is inspected (and pulled if unavailable) for the target GOOS and
// GOARCH
func (bh *buildHelper) targetDocker(dockerImage string) error {
	inspect := exec.Command("docker", "inspect", "-f", "{{.Os}} {{.Architecture}}", dockerImage)
	out, err := inspect.CombinedOutput()
	if err != nil {
		if !bytes.Contains(out, []byte("Error: No such object: "+dockerImage)) {
			return fmt.Errorf("failed to run [%v]: %s\n%s", inspect, err, out)
		}
		// Try a pull
		pull := exec.Command("docker", "pull", dockerImage)
		out, err = pull.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to inspect or pull %s: %v\n%s", dockerImage, err, out)
		}
		inspect := exec.Command("docker", "inspect", "-f", "{{.Os}} {{.Architecture}}", dockerImage)
		out, err = inspect.CombinedOutput()
		if err != nil {
			return fmt.Errorf("pulled but failed to inspect %s: %v\n%s", dockerImage, err, out)
		}
	}
	// Check that we have a single line of output
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if l := len(lines); l != 1 {
		return fmt.Errorf("got %v lines of output from inspect; expected 1. Output was:\n%s", l, out)
	}
	fields := strings.Fields(lines[0])
	if l := len(fields); l != 2 {
		return fmt.Errorf("got %v fields of output from inspect; expected 2. Output was:\n%s", l, out)
	}
	bh.targetGOOS = fields[0]
	bh.targetGOARCH = fields[1]
	return nil
}

func openUnityCache() (*cache.Cache, error) {
	ucd, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine user cache dir: %v", err)
	}
	binCache := filepath.Join(ucd, "unity", "bin")
	if err := os.MkdirAll(binCache, 0777); err != nil {
		return nil, fmt.Errorf("failed to ensure %s exists: %v", binCache, err)
	}
	vcache, err := cache.Open(binCache)
	if err != nil {
		return nil, fmt.Errorf("failed to open cache at %s: %v", binCache, err)
	}
	return vcache, nil
}

// pathToSelf returns the directory within which a compiled version of self
// called "unity" appropriate for running within a docker container exists.
// temp indicates to the caller whether that is in a temporary location that
// must be purged after use. dir is the context within which we are running
// and can find self
func (bh *buildHelper) pathToSelf(selfDir, tempDir string, testing bool) (self string, err error) {
	if !testing && bh.targetGOOS == runtime.GOOS && bh.targetGOARCH == runtime.GOARCH {
		// "self" is sufficient
		self, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("failed to derive path to self: %v", err)
		}
		return self, nil
	}

	// At this point we know that we need to build a version of "self" appropriate
	// for the target docker image. Use the debug.BuildInfo to work out what to do.

	// compileTargetDir indicates where we should ensure self is built
	var compileTargetDir string

	// buildDir is the context within which we will build self
	var buildDir string

	bi, ok := debug.ReadBuildInfo()

	// In the case we have valid build info, determine whether we have
	// valid versions for all modules
	modules, modulesAreValid := buildInfoToModules(bi, ok)

	if !ok || !semver.IsValid(bi.Main.Version) || !modulesAreValid {
		// Assert that we are running in the context of the module that
		// provides the main package. i.e. that the main module is
		// github.com/cue-sh/unity. This is not guaranteed to be the case
		// but for the purposes of what we need this is probably sufficient.
		// If it isn't, then we cross that bridge when we come to it
		var listInfo struct {
			Dir     string
			Version string
		}
		list := exec.Command("go", "list", "-m", "-json", moduleSelf)
		list.Dir = selfDir
		listOut, err := list.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to resolve %s in %s: ran [%v]: %v\n%s", moduleSelf, selfDir, list, err, listOut)
		}
		if err := json.Unmarshal(listOut, &listInfo); err != nil {
			return "", fmt.Errorf("failed to decode list output: %v\n%s", err, listOut)
		}
		if listInfo.Version != "" {
			// We are in the module cache
			compileTargetDir = tempDir
		} else {
			compileTargetDir = filepath.Join(listInfo.Dir, ".bin")
		}
		target := filepath.Join(compileTargetDir, mainName)
		build := exec.Command("go", "build", "-o", target, mainSelf)
		build.Dir = buildDir
		build.Env = append(os.Environ(), bh.buildEnv()...)
		buildOut, err := build.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("failed to build %s; ran [%v]: %v\n%s", mainSelf, build, err, buildOut)
		}
		return target, nil
	}

	if err := bh.writeGoModSum(tempDir, mainSelf, modules); err != nil {
		return "", fmt.Errorf("failed to build temp go.{mod,sum} in %s: %v", tempDir, err)
	}

	buildDir = tempDir
	compileTargetDir = tempDir

	target := filepath.Join(tempDir, mainName)
	if err := bh.buildAndCache(tempDir, target, mainSelf); err != nil {
		return "", fmt.Errorf("failed to build %s: %v", mainSelf, err)
	}
	return target, nil
}

// buildInfoToModules translates bi into a list of modules, reporting also
// whether the list of modules is defined entirely in terms of modules
// that can be resolved from semver versions (i.e. no devel versions, no
// directory sources)
func buildInfoToModules(bi *debug.BuildInfo, ok bool) (modules []*debug.Module, modulesAreValid bool) {
	if !ok {
		return nil, false
	}
	modulesAreValid = true
	modules = append(modules, &bi.Main)
	modules = append(modules, bi.Deps...)
	for _, m := range modules {
		v := m.Version
		if m.Replace != nil {
			v = m.Replace.Version
		}
		if !semver.IsValid(v) {
			modulesAreValid = false
		}
	}
	return
}

func (bh *buildHelper) writeGoModSum(tempDir, mainPkg string, modules []*debug.Module) (err error) {
	// We have build info and the version of self is valid semver
	// Write a temp go.mod and go.sum to tempDir and use that as
	// the buildDir
	type cmdError struct {
		err error
	}
	defer func() {
		switch r := recover().(type) {
		case nil:
		case cmdError:
			err = r.err
		default:
			panic(r)
		}
	}()
	g := func(args ...string) {
		cmd := exec.Command("go")
		cmd.Args = append(cmd.Args, args...)
		cmd.Dir = tempDir
		if out, err := cmd.CombinedOutput(); err != nil {
			panic(cmdError{fmt.Errorf("failed to run [%v] in %s: %v\n%s", cmd, cmd.Dir, err, out)})
		}
	}
	writeFile := func(path string, contents string) {
		path = filepath.Join(tempDir, path)
		if err := ioutil.WriteFile(path, []byte(contents), 0666); err != nil {
			panic(cmdError{fmt.Errorf("failed to write %s: %v", path, err)})
		}

	}
	var gosum bytes.Buffer
	sum := func(m *debug.Module) {
		fmt.Fprintf(&gosum, "%s %s %s\n", m.Path, m.Version, m.Sum)
	}
	g("mod", "init", "unity-temp")
	for _, m := range modules {
		g("mod", "edit", "-require", m.Path+"@"+m.Version)
		sum(m)
		if r := m.Replace; r != nil {
			g("mod", "edit", "-replace", m.Path+"="+r.Path+"@"+r.Version)
			sum(r)
		}
	}
	writeFile("go.sum", gosum.String())
	writeFile("tools.go", fmt.Sprintf(`// +build tools

package tools

import (
	_ "%s"
)
`, mainPkg))
	g("mod", "tidy")
	return
}

func (bh *buildHelper) buildAndCache(buildDir, target, main string) error {
	// Now determine the buildID for this temporary module
	list := exec.Command("go", "list", "-export", "-f={{.BuildID}}", main)
	list.Dir = buildDir
	list.Env = append(os.Environ(), bh.buildEnv()...)
	out, err := list.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to derive buildID for %s: %v\n%s", main, err, out)
	}
	buildID := strings.TrimSpace(string(out))

	// Check we if have a cache entry already
	cacheHash := cache.NewHash("version")
	cacheHash.Write([]byte(buildID))
	if contents, _, err := bh.cache.GetBytes(cacheHash.Sum()); err == nil {
		// cache hit
		if err := os.WriteFile(target, contents, 0777); err != nil {
			return fmt.Errorf("failed to write self to %s: %v", target, err)
		}
		return nil
	}
	build := exec.Command("go", "build", "-o", target, main)
	build.Dir = buildDir
	build.Env = append(os.Environ(), bh.buildEnv()...)
	buildOut, err := build.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to build %s; ran [%v]: %v\n%s", main, build, err, buildOut)
	}
	targetFile, err := os.Open(target)
	if err != nil {
		return fmt.Errorf("failed to open compiled version of self")
	}
	// Write back to the cache
	if _, _, err := bh.cache.Put(cacheHash.Sum(), targetFile); err != nil {
		return fmt.Errorf("failed to write compiled version of self to the cache: %v", err)
	}
	return nil
}

// buildEnv constructs environment variables required
// for building self/CUE for running inside a docker
// container
func (bh *buildHelper) buildEnv() []string {
	return []string{
		"GOOS=" + bh.targetGOOS,
		"GOARCH=" + bh.targetGOARCH,
		"CGO_ENABLED=0",
	}
}

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
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"text/template"

	"golang.org/x/mod/semver"
)

const (
	// cacheVersions is the name of the directory within the unity cache
	// root where CUE versions are cached
	cacheVersions = "versions"
)

// goReleaserBuilder defines the API of the various strategies for mapping
// to from version, GOOS and GOARCH to artefact name
type goReleaserBuilder func(version, goos, goarch string) (string, error)

type versionResolver struct {
	// cacheRoot is the directory within which unity cache
	// information is stored, e.g. the versions subdirectory
	// is the cache of CUE versions used by unity
	cacheRoot string

	// resolvedLock guards acces to resolved
	resolvedLock sync.Mutex

	// resolved is a map of previously resolved versions
	resolved map[string]resolvedVersion

	// oncesLock guards access to onces
	oncesLock sync.Mutex

	// onces captures the once-only semantics of each
	// version we try to resolve
	onces map[string]*sync.Once

	// goReleaserBuilders is a list of functions that map from a semver version
	// to a goreleaser artefact name. This allows us to not be pinned to a
	// single configuration of goreleaser, and instead allows us to try and
	// resolve multiple potential matches simultaneously.
	goReleaserBuilders []goReleaserBuilder

	// semverURLTemplate is the template used to establish the URI of semver
	// assets. See semverURLData for details of valid template fields
	semverURLTemplate *template.Template

	// allowPATH determines whether a CUE version of PATH is allowed or not
	allowPATH bool

	debug bool
}

func (vr *versionResolver) debugf(format string, args ...interface{}) {
	if vr.debug {
		out := fmt.Sprintf(format, args...)
		if out != "" && out[len(out)-1] != '\n' {
			out += "\n"
		}
		fmt.Fprint(os.Stderr, out)
	}
}

func (vr *versionResolver) buildURL(version string, artefact string) (*url.URL, error) {
	tmplData := semverURLData{
		Version:  version,
		Artefact: artefact,
	}
	var buf bytes.Buffer
	if err := vr.semverURLTemplate.Execute(&buf, tmplData); err != nil {
		return nil, fmt.Errorf("failed to execute template: %v", err)
	}
	u, err := url.Parse(buf.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse %q as a URL: %v", buf.String(), err)
	}
	return u, nil
}

type semverURLData struct {
	// Version is the version requested
	Version string

	// Artefact is the file name of the artefact being requested, which is
	// built separately by the goReleaserBuilders
	Artefact string
}

func newVersionResolver(allowPATH bool) (*versionResolver, error) {
	ucd, err := os.UserCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to determine user cache dir: %v", err)
	}
	cacheRoot := filepath.Join(ucd, "cue-unity")
	if err := os.MkdirAll(cacheRoot, 0777); err != nil {
		return nil, fmt.Errorf("failed to create unity CUE version cache dir: %v", err)
	}
	urlTmpl := os.Getenv("UNITY_SEMVER_URL_TEMPLATE")
	if urlTmpl == "" {
		urlTmpl = "https://github.com/cuelang/cue/releases/download/{{.Version}}/{{.Artefact}}"
	}
	t, err := template.New("tmpl").Parse(urlTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse semver URL template %q: %v", urlTmpl, err)
	}
	res := &versionResolver{
		cacheRoot:         cacheRoot,
		resolved:          make(map[string]resolvedVersion),
		onces:             make(map[string]*sync.Once),
		semverURLTemplate: t,
		allowPATH:         allowPATH,
		goReleaserBuilders: []goReleaserBuilder{
			buildOldStyleGoreleaser(),
			buildNewStyleGoreleaser(),
		},
	}
	u, err := res.buildURL("v", "a")
	if err != nil {
		return nil, fmt.Errorf("failed to verify semver URL template: %v", err)
	}
	switch u.Scheme {
	case "file", "https":
	default:
		return nil, fmt.Errorf("unsupported semver URL template scheme: %q", u.Scheme)
	}
	return res, nil
}

type resolvedVersion struct {
	path string
	err  error
}

var (
	errPATHNotAllowed = errors.New("CUE version of PATH not permitted")
)

// resolve interprets version as either:
//
// 1. semver version
// 2. PATH (use cue from PATH)
// 3. ...
//
// and returns a path to the cue binary corresponding to that
// version. The path will sit within the unity cache.
func (vr *versionResolver) resolve(version string) (path string, err error) {

	// TODO add support for multiple version resolution algorithms
	// Note these are just prelimnary checks aimed at identifying
	// the type of version that we have been supplied, i.e. semver
	// CL/patchset etc. Until we actually try and resolve such a version
	// we can't know if it's valid or not
	var resolve func(string) (string, error)
	switch {
	case version == "PATH":
		if !vr.allowPATH {
			return "", errPATHNotAllowed
		}
	case semver.IsValid(version):
		resolve = vr.resolveSemverImpl
	default:
		return "", fmt.Errorf("unknown version format %q", version)
	}

	// Special case - no resolution required. Arguably
	// this could live below but hey
	if version == "PATH" {
		path, err := exec.LookPath("cue")
		if err != nil {
			err = fmt.Errorf("failed to lookup cue in PATH: %v", err)
		}
		return path, err
	}

	vr.resolvedLock.Lock()
	rv, resolved := vr.resolved[version]
	vr.resolvedLock.Unlock()
	if resolved {
		return rv.path, rv.err
	}

	vr.oncesLock.Lock()
	once, ok := vr.onces[version]
	if !ok {
		once = new(sync.Once)
		vr.onces[version] = once
	}
	vr.oncesLock.Unlock()
	did := false
	once.Do(func() {
		did = true
		path, err = resolve(version)
		vr.resolvedLock.Lock()
		vr.resolved[version] = resolvedVersion{
			path: path,
			err:  err,
		}
		vr.resolvedLock.Unlock()
	})
	if did { // minor optimisation
		return path, err
	}
	vr.resolvedLock.Lock()
	rv, resolved = vr.resolved[version]
	vr.resolvedLock.Unlock()
	if !resolved {
		panic("oh dear")
	}
	return rv.path, rv.err
}

// resolveSemverImpl is responsible for resolving a valid semver version to a
// CUE version (if one exists) or an error
func (vr *versionResolver) resolveSemverImpl(version string) (string, error) {
	// TODO: support semver sources other than "GitHub with artefacts built by
	// goreleaser"

	versionPath := filepath.Join(vr.cacheRoot, cacheVersions, version)
	if err := os.MkdirAll(versionPath, 0777); err != nil {
		return "", fmt.Errorf("failed to mkdir %s: %v", versionPath, err)
	}
	cuePath := filepath.Join(versionPath, "cue")
	if _, err := os.Stat(cuePath); err == nil {
		return cuePath, nil
	}

	var urls []*url.URL
	for _, gbs := range vr.goReleaserBuilders {
		a, err := gbs(version, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return "", fmt.Errorf("failed to resolve")
		}
		u, err := vr.buildURL(version, a)
		if err != nil {
			return "", fmt.Errorf("failed to build URL for version %q artefact %q: %v", version, a, err)
		}
		urls = append(urls, u)
	}
	type respErr struct {
		url  *url.URL
		body io.ReadCloser
		err  error
	}
	resps := make([]respErr, len(urls))
	var wg sync.WaitGroup
	for i, u := range urls {
		i, u := i, u
		wg.Add(1)
		go func() {
			defer wg.Done()
			var body io.ReadCloser
			var err error
			switch u.Scheme {
			case "file":
				vr.debugf("open file %s", u.Path)
				body, err = os.Open(u.Path)
			case "https":
				resp, geterr := http.Get(u.String())
				err = geterr
				if err == nil {
					body = resp.Body
					if resp.StatusCode/100 != 2 {
						err = errors.New(resp.Status)
					}
				}
			default:
				panic("should not get here because of scheme check in newVersionResolver")
			}
			resps[i] = respErr{
				url:  u,
				body: body,
				err:  err,
			}
		}()
	}
	wg.Wait()

	var resp respErr
	successCount := 0
	for _, re := range resps {
		if re.err == nil {
			successCount++
			resp = re
			defer re.body.Close()
		}
	}
	if successCount != 1 {
		return "", fmt.Errorf("failed to resolve %q to a single successful response", version)
	}
	archive, err := gzip.NewReader(resp.body)
	if err != nil {
		return "", fmt.Errorf("failed to create gzip reader for response: %v", err)
	}
	t := tar.NewReader(archive)
	foundCue := false
	for {
		h, err := t.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return "", fmt.Errorf("failed to read tar archive: %v", err)
		}
		if h.Name == "cue" {
			foundCue = true
			lr := io.LimitReader(t, h.Size)
			// TODO probably need lockedfile here to safely write exclusively
			of, err := os.OpenFile(cuePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0777)
			if err != nil {
				return "", fmt.Errorf("failed to open %v for writing: %v", cuePath, err)
			}
			if n, err := io.Copy(of, lr); err != nil || n != h.Size {
				return "", fmt.Errorf("failed to write output to %v: wrote %v of %v, with err: %v", cuePath, n, h.Size, err)
			}
		}
	}
	if !foundCue {
		return "", fmt.Errorf("%s did not contain cue binary", resp.url)
	}
	return cuePath, nil
}

func buildOldStyleGoreleaser() func(version, goos, goarch string) (string, error) {
	var (
		// goreleaser mappings taken from .goreleaser.yml in cue repo

		goReleaserGOOSMappings = map[string]string{
			"linux":   "Linux",
			"darwin":  "Darwin",
			"windows": "Windows",
		}
		goReleaserGOARCHMappings = map[string]string{
			"386":   "i386",
			"amd64": "x86_64",
		}
	)
	return func(version, goos, goarch string) (string, error) {
		var ok bool
		goos, ok = goReleaserGOOSMappings[goos]
		if !ok {
			return "", fmt.Errorf("old-style strategy: unknown GOOS %q", runtime.GOOS)
		}
		goarch, ok = goReleaserGOARCHMappings[goarch]
		if !ok {
			return "", fmt.Errorf("old-style strategy: unknown GOARCH %q", runtime.GOARCH)
		}
		// drop v prefix
		version = version[1:]
		return fmt.Sprintf("cue_%v_%v_%v.tar.gz", version, goos, goarch), nil
	}

}

func buildNewStyleGoreleaser() func(version, goos, goarch string) (string, error) {
	return func(version, goos, goarch string) (string, error) {
		return fmt.Sprintf("cue_%v_%v_%v.tar.gz", version, goos, goarch), nil
	}
}

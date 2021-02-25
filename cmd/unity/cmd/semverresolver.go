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
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"text/template"

	"cuelang.org/go/cue/errors"
	"golang.org/x/mod/semver"
)

// semverResolver resolves valid semver CUE versions to official builds
// that are released onto GitHub. The results of GitHub downloads are cached
// in the user cache directory.
type semverResolver struct {
	config resolverConfig

	// urlTemplate is the template used to establish the URI of semver assets.
	// See semverURLData for details of valid template fields
	urlTemplate *template.Template

	// goReleaserBuilders is a list of functions that map from a semver version
	// to a goreleaser artefact name. This allows us to not be pinned to a
	// single configuration of goreleaser, and instead allows us to try and
	// resolve multiple potential matches simultaneously.
	goReleaserBuilders []goReleaserBuilder

	// oncesLock guards access to onces
	oncesLock sync.Mutex

	// onces captures the once-only semantics of each
	// version we try to resolve
	onces map[[32]byte]*sync.Once
}

// goReleaserBuilder defines the API of the various strategies for mapping
// to from version, GOOS and GOARCH to artefact name
type goReleaserBuilder func(version, goos, goarch string) (string, error)

var _ resolver = (*semverResolver)(nil)

func newSemverResolver(c resolverConfig) (resolver, error) {
	urlTmpl := os.Getenv("UNITY_SEMVER_URL_TEMPLATE")
	if urlTmpl == "" {
		urlTmpl = "https://github.com/cuelang/cue/releases/download/{{.Version}}/{{.Artefact}}"
	}
	t, err := template.New("tmpl").Parse(urlTmpl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse semver URL template %q: %v", urlTmpl, err)
	}
	res := &semverResolver{
		config:      c,
		urlTemplate: t,
		onces:       make(map[[32]byte]*sync.Once),
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

// buildURL creates a *url.URL from the supplied version and artefact identifiers,
// according to vr.semverURLTemplate. This allows us to have alternative sources
// for semver
func (sr *semverResolver) buildURL(version string, artefact string) (*url.URL, error) {
	tmplData := semverURLData{
		Version:  version,
		Artefact: artefact,
	}
	var buf bytes.Buffer
	if err := sr.urlTemplate.Execute(&buf, tmplData); err != nil {
		return nil, fmt.Errorf("failed to execute template: %v", err)
	}
	u, err := url.Parse(buf.String())
	if err != nil {
		return nil, fmt.Errorf("failed to parse %q as a URL: %v", buf.String(), err)
	}
	return u, nil
}

func (sr *semverResolver) resolve(version, dir, working, targetDir string) error {
	if !semver.IsValid(version) {
		return errNoMatch
	}
	h := sr.config.bh.cueVersionHash(version)
	key := h.Sum()
	ce, _, err := sr.config.bh.cache.GetFile(key)
	if err == nil {
		return copyExecutableFile(ce, filepath.Join(targetDir, "cue"))
	}
	sr.oncesLock.Lock()
	once, ok := sr.onces[key]
	if !ok {
		once = new(sync.Once)
		sr.onces[key] = once
	}
	sr.oncesLock.Unlock()
	var onceerr error
	once.Do(func() {
		onceerr = sr.resolveSemverImpl(key, version)
	})
	if onceerr != nil {
		return fmt.Errorf("failed to download %s: %v", version, onceerr)
	}
	ce, _, err = sr.config.bh.cache.GetFile(key)
	if err != nil {
		return fmt.Errorf("failed to resolve %s from cache after download", version)
	}
	return copyExecutableFile(ce, filepath.Join(targetDir, "cue"))
}

type semverURLData struct {
	// Version is the version requested
	Version string

	// Artefact is the file name of the artefact being requested, which is
	// built separately by the goReleaserBuilders
	Artefact string
}

// resolveSemverImpl is responsible for resolving a valid semver version to a
// CUE version (if one exists) or an error
func (sr *semverResolver) resolveSemverImpl(key [32]byte, version string) error {
	// TODO: support semver sources other than "GitHub with artefacts built by
	// goreleaser"

	var urls []*url.URL
	for _, gbs := range sr.goReleaserBuilders {
		a, err := gbs(version, runtime.GOOS, runtime.GOARCH)
		if err != nil {
			return fmt.Errorf("failed to resolve")
		}
		u, err := sr.buildURL(version, a)
		if err != nil {
			return fmt.Errorf("failed to build URL for version %q artefact %q: %v", version, a, err)
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
				sr.config.debugf("open file %s", u.Path)
				body, err = os.Open(u.Path)
			case "https":
				sr.config.debugf("get %s", u.String())
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
		return fmt.Errorf("failed to resolve %q to a single successful response", version)
	}
	archive, err := gzip.NewReader(resp.body)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader for response: %v", err)
	}
	t := tar.NewReader(archive)
	foundCue := false
	for {
		h, err := t.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("failed to read tar archive: %v", err)
		}
		if h.Name == "cue" {
			foundCue = true
			cueBin := make([]byte, h.Size)
			if _, err := io.ReadFull(t, cueBin); err != nil {
				return fmt.Errorf("failed to read cue from tar archive: %v", err)
			}
			if err := sr.config.bh.cache.PutBytes(key, cueBin); err != nil {
				return fmt.Errorf("failed to write cue to the cache: %v", err)
			}
			break
		}
	}
	if !foundCue {
		return fmt.Errorf("%s did not contain cue binary", resp.url)
	}
	return nil
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

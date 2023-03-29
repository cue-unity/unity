// Copyright 2022 The CUE Authors
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

package github

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The trybot workflow.
workflows: trybot: _base.#bashWorkflow & {
	// Note: the name of this workflow is used by gerritstatusupdater as an
	// identifier in the status updates that are posted as reviews for this
	// workflows, but also as the result label key, e.g. "TryBot-Result" would
	// be the result label key for the "TryBot" workflow. Note the result label
	// key is therefore tied to the configuration of this repository.
	name: "TryBot"

	on: {
		push: {
			branches: ["trybot/*/*/*/*", _repo.defaultBranch, _base.#testDefaultBranch] // do not run PR branches
			"tags-ignore": [_repo.releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_base.#installGo,
				_base.#checkoutCode & {
					// "pull_request" builds will by default use a merge commit,
					// testing the PR's HEAD merged on top of the master branch.
					// For consistency with Gerrit, avoid that merge commit entirely.
					// This doesn't affect "push" builds, which never used merge commits.
					with: ref:        "${{ github.event.pull_request.head.sha }}"
					with: submodules: true
				},
				_base.#earlyChecks & {
					// These checks don't vary based on the Go version or OS,
					// so we only need to run them on one of the matrix jobs.
					if: "\(_repo.isLatestLinux)"
				},
				_base.#cacheGoModules,
				json.#step & {
					if:  "\(_base.#isDefaultBranch)"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_#goModVerify,
				_#goGenerate,
				_#goTest,
				_#goCheck,
				_#goTestRace & {
					if: "${{ \(_base.#isDefaultBranch) }}"
				},
				_#staticcheck,
				_#goModTidy,
				_base.#checkGitClean,
				_#installUnity,
				_#runUnity,
			]
		}
	}

	_#goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: "\(_repo.isLatestLinux)"
	}

	_#goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_#goCheck: json.#step & {
		// These checks can vary between platforms, as different code can be built
		// based on GOOS and GOARCH build tags.
		// However, CUE does not have any such build tags yet, and we don't use
		// dependencies that vary wildly between platforms.
		// For now, to save CI resources, just run the checks on one matrix job.
		// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
		if:   "\(_repo.isLatestLinux)"
		name: "Check"
		run:  "go vet ./..."
	}

	_#goTestRace: json.#step & {
		name: "Test with -race"
		run:  "go test -race ./..."
	}

	_#goModVerify: json.#step & {
		name: "go mod verify"
		run:  "go mod verify"
	}

	_#goModTidy: json.#step & {
		name: "go mod tidy"
		run:  "go mod tidy"
	}

	_#staticcheck: json.#step & {
		name: "staticcheck"
		run:  "go run honnef.co/go/tools/cmd/staticcheck@v0.3.3 ./..."
	}
}

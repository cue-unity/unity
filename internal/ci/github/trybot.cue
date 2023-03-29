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
	"list"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// The trybot workflow.
workflows: trybot: _repo.bashWorkflow & {
	name: _repo.trybot.name

	on: {
		push: {
			branches: list.Concat([[
					"trybot/*/*/*/*",
					_repo.testDefaultBranch,
			], _repo.protectedBranchPatterns,
			])
			"tags-ignore": [_repo.releaseTagPattern]
		}
		pull_request: {}
	}

	jobs: {
		test: {
			steps: [
				for v in _checkoutCode {v},

				// Early git checks
				_repo.earlyChecks,

				_installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _setupGoActionsCaches {v},

				json.#step & {
					if:  "\(_repo.isProtectedBranch)"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
				},
				_goModVerify,
				_goGenerate,
				_goTest,
				_goCheck,
				_goTestRace & {
					if: "${{ \(_repo.isProtectedBranch) }}"
				},
				_staticcheck,
				_goModTidy,
				_repo.checkGitClean,
				_installUnity,
				_runUnity,
			]
		}
	}
	// _isLatestLinux evaluates to true if the job is running on Linux with the
	// latest version of Go. This expression is often used to run certain steps
	// just once per CI workflow, to avoid duplicated work.
	_isLatestLinux: "matrix.go-version == '\(_repo.latestStableGo)' && matrix.os == '\(_repo.linuxMachine)'"

	_goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
		// The Go version corresponds to the precise version specified in
		// the matrix. Skip windows for now until we work out why re-gen is flaky
		if: "\(_isLatestLinux)"
	}

	_goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_goCheck: json.#step & {
		// These checks can vary between platforms, as different code can be built
		// based on GOOS and GOARCH build tags.
		// However, CUE does not have any such build tags yet, and we don't use
		// dependencies that vary wildly between platforms.
		// For now, to save CI resources, just run the checks on one matrix job.
		// TODO: consider adding more checks as per https://github.com/golang/go/issues/42119.
		if:   "\(_isLatestLinux)"
		name: "Check"
		run:  "go vet ./..."
	}

	_goTestRace: json.#step & {
		name: "Test with -race"
		run:  "go test -race ./..."
	}

	_goModVerify: json.#step & {
		name: "go mod verify"
		run:  "go mod verify"
	}

	_goModTidy: json.#step & {
		name: "go mod tidy"
		run:  "go mod tidy"
	}

	_staticcheck: json.#step & {
		name: "staticcheck"
		run:  "go run honnef.co/go/tools/cmd/staticcheck@v0.4.3 ./..."
	}
}

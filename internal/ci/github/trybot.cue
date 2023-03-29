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
	_goGenerate: json.#step & {
		name: "Generate"
		run:  "go generate ./..."
	}

	_goTest: json.#step & {
		name: "Test"
		run:  "go test ./..."
	}

	_goCheck: json.#step & {
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

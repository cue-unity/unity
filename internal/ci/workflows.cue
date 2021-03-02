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

package ci

import (
	"github.com/SchemaStore/schemastore/src/schemas/json"
	encjson "encoding/json"
)

workflowsDir: *"./" | string @tag(workflowsDir)

_#mainBranch:        "main"
_#releaseTagPattern: "v*"

workflows: [...{file: string, schema: (json.#Workflow & {})}]
workflows: [
	{
		file:   "test.yml"
		schema: test
	},
	{
		file:   "daily_check.yml"
		schema: dailycheck
	},
	{
		file:   "dispatch.yml"
		schema: dispatch
	},
]

test: _#bashWorkflow & {

	name: "Test"
	on: {
		push: {
			branches: ["main"]
			tags: [_#releaseTagPattern]
		}
		pull_request: branches: ["**"]
	}

	jobs: {
		test: {
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_#installGo,
				_#checkoutCode,
				_#cacheGoModules,
				_#setGoBuildTags & {
					_#tags: "long"
					if:     "${{ \(_#isMain) }}"
				},
				_#goModVerify,
				_#goGenerate,
				_#goTest,
				_#goTestRace & {
					if: "${{ \(_#isMain) }}"
				},
				_#staticcheck,
				_#goModTidy,
				_#checkGitClean,
				_#runUnity,
			]
		}
	}
}

dailycheck: _#bashWorkflow & {

	name: "Daily check"
	on: {
		schedule: [{cron: "0 9 * * *"}]
	}

	jobs: {
		test: {
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_#installGo,
				_#checkoutCode,
				_#runUnity,
			]
		}
	}
}

dispatch: _#bashWorkflow & {
	// These constants are defined by github.com/cue-sh/tools/cmd/cueckoo
	_#runtrybot: "runtrybot"
	_#mirror:    "mirror"
	_#importpr:  "importpr"
	_#unity:     "unity"

	_#dispatchJob: _#job & {
		_#type: string
		if:     "${{ github.event.client_payload.type == '\(_#type)' }}"
	}

	name: "Repository Dispatch"
	on: ["repository_dispatch"]

	jobs: {
		test: _#dispatchJob & {
			_#type:    _#unity
			strategy:  _#testStrategy
			"runs-on": "${{ matrix.os }}"
			steps: [
				_#writeCookiesFile & {
					if: "${{ \(_#ifIsCLVersion) }}"
				},
				_#startCLBuild & {
					if: "${{ \(_#ifIsCLVersion) }}"
				},
				_#installGo,
				_#checkoutCode,
				_#cacheGoModules,
				_#step & {
					name: "Run unity"
					run: """
						echo "${{ github.event.client_payload.payload.versions }}" | xargs ./_scripts/runUnity.sh"
						"""
				},
				_#failCLBuild & {
					if: "${{ \(_#ifIsCLVersion) && failure() }}"
				},
				_#passCLBuild & {
					if: "${{ \(_#ifIsCLVersion) && success() }}"
				},
			]
		}
	}

	_#ifIsCLVersion: "github.event.client_payload.payload.cl != null"

	_#startCLBuild: _#step & {
		name: "Update Gerrit CL message with starting message"
		run:  (_#gerrit._#setCodeReview & {
			#args: message: "Started the build... see progress at ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
		}).res
	}

	_#failCLBuild: _#step & {
		name: "Post any failures for this matrix entry"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Build failed for ${{ runner.os }}-${{ matrix.go-version }}; see ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }} for more details"
				labels: {
					"Code-Review": -1
				}
			}
		}).res
	}

	_#passCLBuild: _#step & {
		name: "Update Gerrit CL message with success message"
		run:  (_#gerrit._#setCodeReview & {
			#args: {
				message: "Build succeeded for ${{ github.event.repository.html_url }}/actions/runs/${{ github.run_id }}"
				labels: {
					"Code-Review": 1
				}
			}
		}).res
	}

	_#gerrit: {
		_#setCodeReview: {
			#args: {
				message: string
				labels?: {
					"Code-Review": int
				}
			}
			res: #"""
			curl -f -s -H "Content-Type: application/json" --request POST --data '\#(encjson.Marshal(#args))' -b ~/.gitcookies https://cue-review.googlesource.com/a/changes/${{ github.event.client_payload.payload.cl.changeID }}/revisions/${{ github.event.client_payload.payload.cl.commit }}/review
			"""#
		}
	}
}

_#bashWorkflow: json.#Workflow & {
	jobs: [string]: defaults: run: shell: "bash"
}

// TODO: drop when cuelang.org/issue/390 is fixed.
// Declare definitions for sub-schemas
_#job:  ((json.#Workflow & {}).jobs & {x: _}).x
_#step: ((_#job & {steps:                 _}).steps & [_])[0]

// Use a specific latest version for release builds
_#latestStableGo: "1.16"
_#codeGenGo:      _#latestStableGo

_#linuxMachine:   "ubuntu-18.04"
_#macosMachine:   "macos-10.15"
_#windowsMachine: "windows-2019"

_#writeCookiesFile: _#step & {
	name: "Write the gitcookies file"
	run:  "echo \"${{ secrets.gerritCookie }}\" > ~/.gitcookies"
}

_#testStrategy: {
	"fail-fast": false
	matrix: {
		"go-version": [_#latestStableGo]
		os: [_#linuxMachine]
	}
}

_#setGoBuildTags: _#step & {
	_#tags: string
	name:   "Set go build tags"
	run:    """
		go env -w GOFLAGS=-tags=\(_#tags)
		"""
}

_#installGo: _#step & {
	name: "Install Go"
	uses: "actions/setup-go@v2"
	with: {
		"go-version": *"${{ matrix.go-version }}" | string
		stable:       false
	}
}

_#checkoutCode: _#step & {
	name: "Checkout code"
	uses: "actions/checkout@v2"
	with: {
		submodules: true
	}
}

_#cacheGoModules: _#step & {
	name: "Cache Go modules"
	uses: "actions/cache@v1"
	with: {
		path: "~/go/pkg/mod"
		key:  "${{ runner.os }}-${{ matrix.go-version }}-go-${{ hashFiles('**/go.sum') }}"
		"restore-keys": """
			${{ runner.os }}-${{ matrix.go-version }}-go-
			"""
	}
}

_#goModVerify: _#step & {
	name: "go mod verify"
	run:  "go mod verify"
}

_#goModTidy: _#step & {
	name: "go mod tidy"
	run:  "go mod tidy"
}

_#goGenerate: _#step & {
	name: "Generate"
	run:  "go generate ./..."
	// The Go version corresponds to the precise version specified in
	// the matrix. Skip windows for now until we work out why re-gen is flaky
	if: "matrix.go-version == '\(_#codeGenGo)' && matrix.os != '\(_#windowsMachine)'"
}

_#staticcheck: _#step & {
	name: "staticcheck"
	run:  "go run honnef.co/go/tools/cmd/staticcheck ./..."
}

_#goTest: _#step & {
	name: "Test"
	run:  "go test -count=1 ./..."
}

_#goTestRace: _#step & {
	name: "Test with -race"
	run:  "go test -count=1 -race ./..."
}

_#checkGitClean: _#step & {
	name: "Check that git is clean post generate and tests"
	run:  "test -z \"$(git status --porcelain)\" || (git status; git diff; false)"
}

_#runUnity: _#step & {
	name: "Run unity"
	run:  "./_scripts/runUnity.sh"
}

_#branchRefPrefix: "refs/heads/"

// _#isCLCITestBranch is an expression that evaluates to true
// if the job is running as a result of a CL triggered CI build
_#isCLCITestBranch: "startsWith(github.ref, '\(_#branchRefPrefix)ci/')"

// _#isMain is an expression that evaluates to true if the
// job is running as a result of a main commit push
_#isMain: "github.ref == '\(_#branchRefPrefix+_#mainBranch)'"

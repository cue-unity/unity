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

// unity is the workflow where triggered by cmd/cueckoo runtrybot for a CUE
// project CL. This workflow runs in the trybot repo, and webhook events update
// the source CUE project CL.
unity: _base.#bashWorkflow & {
	name: "Unity"

	on: {
		push: {
			branches: ["unity/*/*"] // only run on unity build branches
		}
	}

	jobs: {
		test: {
			strategy:          _#testStrategy
			"timeout-minutes": 15
			"runs-on":         "${{ matrix.os }}"
			steps: [
				_base.#installGo,
				_base.#checkoutCode & {
					with: submodules: true
				},
				_base.#cacheGoModules,
				_#installUnity,
				json.#step & {
					name: "Run unity"
					run: """
						./_scripts/runUnity.sh change:$(basename $(dirname $GITHUB_REF))/$(basename $GITHUB_REF)
						"""
				},
			]
		}
	}
}

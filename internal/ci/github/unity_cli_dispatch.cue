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

// unity_cli_dispatch is the "regular" repository dispatch triggered by
// cmd/cueckkoo unity. This supplies arbitrary versions to be tested.
// For such a trigger, the payload.cl field is null.
workflows: unity_cli_dispatch: _repo.bashWorkflow & {
	on: ["repository_dispatch"]
	jobs: {
		test: {
			if: "${{ github.event.client_payload.type == '\(_repo.unity.key)' && github.event.client_payload.payload.cl == null }}"
			steps: [
				for v in _checkoutCode {v},

				_installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _setupReadonlyGoActionsCaches {v},

				_installUnity,

				json.#step & {
					name: "Run unity"
					id:   "unity_run"
					run: """
						set -o pipefail
						echo ${{ toJson(github.event.client_payload.payload.versions) }} | xargs ./_scripts/runUnity.sh
						"""
				},
			]
		}
	}
}

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

// unity_dispatch is the repository dispatch for CLs triggered by
// cmd/cueckoo runtrybot. Therefore, the payload.cl payload will be
// set, which is the identifying feature as far as this dispatch
// is concerned, compared to unity_cli.
workflows: unity_dispatch: _repo.bashWorkflow & {
	_branchNameExpression: "\(_repo.unity.key)/${{ github.event.client_payload.payload.cl.changeID }}/${{ github.event.client_payload.payload.cl.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                  "Dispatch \(_repo.unity.key)"
	on: ["repository_dispatch"]
	jobs: {
		"\(_repo.unity.key)": {
			if: "${{ github.event.client_payload.type == '\(_repo.unity.key)' && github.event.client_payload.payload.cl != null}}"
			steps: [
				// We do not need submodules in this workflow
				for v in _repo.checkoutCode {v},

				// Out of the entire ref (e.g. refs/changes/38/547738/7) we only
				// care about the CL number and patchset, (e.g. 547738/7).
				// Note that gerrithub_ref is two path elements.
				json.#step & {
					id: "gerrithub_ref"
					run: #"""
						ref="$(echo ${{github.event.client_payload.payload.cl.ref}} | sed -E 's/^refs\/changes\/[0-9]+\/([0-9]+)\/([0-9]+).*/\1\/\2/')"
						echo "gerrithub_ref=$ref" >> $GITHUB_OUTPUT
						"""#
				},
				json.#step & {
					name: "Trigger \(_repo.unity.key)"
					run:  """
						git config user.name \(_repo.botGitHubUser)
						git config user.email \(_repo.botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(_repo.botGitHubUser):${{ secrets.\(_repo.botGitHubUserTokenSecretsKey) }} | base64)"
						git checkout -b \(_branchNameExpression)
						git push \(_repo.trybotRepositoryURL) \(_branchNameExpression)
						"""
				},
			]
		}
	}
}

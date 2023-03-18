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
unity_dispatch: _base.#bashWorkflow & {
	_#type:                 _gerrithub.#dispatchUnity
	_#branchNameExpression: "\(_#type)/${{ github.event.client_payload.payload.cl.changeID }}/${{ github.event.client_payload.payload.cl.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                   "Dispatch \(_#type)"
	on: ["repository_dispatch"]
	jobs: {
		"\(_#type)": {
			"runs-on": _#linuxMachine
			if:        "${{ github.event.client_payload.type == '\(_#type)' && github.event.client_payload.payload.cl != null}}"
			steps: [
				// This workflow is triggered against the tip of the default branch.
				// We want to create a branch named unity/$changeID/$revisionID
				// and push that to the trybot repository to trigger the unity
				// workflow. We need full history in order to be able to push
				// to the unity-trybot repository.
				_base.#checkoutCode & {
					with: {
						"fetch-depth": 0
					}
				},
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
					name: "Trigger \(_#type)"
					run:  """
						git config user.name \(_gerrithub.#botGitHubUser)
						git config user.email \(_gerrithub.#botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(_gerrithub.#botGitHubUser):${{ secrets.\(_gerrithub.#botGitHubUserTokenSecretsKey) }} | base64)"
						git checkout -b \(_#branchNameExpression)
						git push \(_gerrithub.#trybotRepositoryURL) \(_#branchNameExpression)
						"""
				},
			]
		}
	}
}

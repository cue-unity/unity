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
	encjson "encoding/json"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

// unity_dispatch is the repository dispatch for CLs triggered by
// cmd/cueckoo runtrybot. Therefore, the payload.cl payload will be
// set, which is the identifying feature as far as this dispatch
// is concerned, compared to unity_cli.
workflows: unity_dispatch: _repo.bashWorkflow & {
	_branchNameExpression: "\(_repo.unity.key)/${{ github.event.client_payload.payload.cl.changeID }}/${{ github.event.client_payload.payload.cl.commit }}/${{ steps.gerrithub_ref.outputs.gerrithub_ref }}"
	name:                  "Dispatch \(_repo.unity.key)"
	on: {
		repository_dispatch: {}
		push: {
			// To enable testing of the dispatch itself
			branches: [_repo.testDefaultBranch]
		}
	}
	jobs: {
		"\(_repo.unity.key)": {
			if: "${{ github.ref == 'refs/heads/\(_repo.testDefaultBranch)' || github.event.client_payload.type == '\(_repo.unity.key)' }}"

			// See the comment below about the need for cases
			let cases = [
				{
					condition:  "!="
					expr:       "fromJSON(steps.payload.outputs.value)"
					nameSuffix: "fake data"
				},
				{
					condition:  "=="
					expr:       "github.event.client_payload"
					nameSuffix: "repository_dispatch payload"
				},
			]

			// Hard-code the unity dispatch to the CL that is top-of-stack
			// in the main CUE repo, at a hardcoded patchset too.
			_unityDummyDispatch: _repo.#dispatch & {
				type:         _repo.unity.key
				CL:           551352
				patchset:     139
				targetBranch: "master"
				ref:          "refs/changes/\(mod(CL, 100))/\(CL)/\(patchset)"
			}

			steps: [
				_repo.writeNetrcFile,

				json.#step & {
					name: "Write fake payload"
					id:   "payload"
					if:   "github.repository == '\(_repo.githubRepositoryPath)' && github.ref == 'refs/heads/\(_repo.testDefaultBranch)'"
					run:  #"""
						cat <<EOD >> $GITHUB_OUTPUT
						value<<DOE
						\#(encjson.Marshal(_unityDummyDispatch))
						DOE
						EOD
						"""#
				},

				// We do not need submodules in this workflow
				for v in _repo.checkoutCode {v},

				// GitHub does not allow steps with the same ID, even if (by virtue
				// of runtime 'if' expressions) both would not actually run. So
				// we have to duplciate the steps that follow with those same
				// runtime expressions
				//
				// Hence we have to create two steps, one to trigger if the
				// repository_dispatch payload is set, and one if not (i.e. we use
				// the fake payload).
				for v in cases {
					json.#step & {
						name: "Trigger \(_repo.unity.name) (\(v.nameSuffix))"
						if:   "github.event.client_payload.type \(v.condition) '\(_repo.unity.key)'"
						run:  """
						set -x

						# We already have the code checked out at the right place.
						# Just need to add the Dispatch-Trailer Note that what we
						# will have checked out here is the tip of the default
						# branch.

						git config user.name \(_repo.botGitHubUser)
						git config user.email \(_repo.botGitHubUserEmail)
						git config http.https://github.com/.extraheader "AUTHORIZATION: basic $(echo -n \(_repo.botGitHubUser):${{ secrets.\(_repo.botGitHubUserTokenSecretsKey) }} | base64)"

						# Error if we already have dispatchTrailer according to git log logic.
						x="$(git log -1 --pretty='%(trailers:key=\(_repo.dispatchTrailer),valueonly)')"
						if [ "$x" != "" ]
						then
							 echo "Ref ${{ \(v.expr).ref }} already has a \(_repo.dispatchTrailer)"
							 exit 1
						fi

						# Add the trailer because we don't have it yet. GitHub expressions do not have a
						# substitute or quote capability. So we do that in shell. We also strip out the
						# indenting added by toJSON. We ensure that the type field is first in order
						# that we can safely check for specific types of dispatch trailer.
						trailer="$(cat <<EOD | jq -c '{type} + .'
						${{ toJSON(\(v.expr)) }}
						EOD
						)"
						git log -1 --format=%B | git interpret-trailers --trailer "\(_repo.dispatchTrailer): $trailer" | git commit --amend -F -
						git log -1

						success=false
						for try in {1..20}; do
							echo "Push to trybot try $try"
							if git push -f \(_repo.trybotRepositoryURL) HEAD:\(_repo.defaultBranch); then
								success=true
								break
							fi
							sleep 1
						done
						if ! $success; then
							echo "Giving up"
							exit 1
						fi
						"""
					}
				},
			]
		}
	}
}

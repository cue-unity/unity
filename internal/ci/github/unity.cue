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
workflows: unity: _repo.bashWorkflow & {
	name: "Unity"

	on: {
		push: {
			branches: ["unity/*/*/*/*"] // only run on unity build branches
		}
	}

	jobs: {
		test: {
			"timeout-minutes": 15
			steps: [
				for v in _checkoutCode {v},

				_installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _setupReadonlyGoActionsCaches {v},

				_installUnity,

				json.#step & {
					name: "Run unity"
					run: """
						# GITHUB_REF is in the form of:
						# "refs/heads/unity/$change_id/$commit/$cl_number/$ps_number".
						# When a user runs "cueckoo runtrybot", it triggers a
						# repository dispatch on the main unity repository,
						# which then pushes the branch unity/... to the
						# cue-trybot repository, triggering the workflow here.
						# The commit (the third element) is enough to do a git fetch.
						commit=$(echo $GITHUB_REF | cut -d "/" -f 5)

						dir_head=$PWD/checkout_head
						dir_parent=$PWD/checkout_parent

						# Initialize an empty git repo and fetch the CL.
						# depth=2 is enough for HEAD and its parent.
						# Make a copy for the parent checkout to reuse the fetch.
						mkdir $dir_head
						cd $dir_head
						git init
						git fetch --depth=2 https://review.gerrithub.io/cue-lang/cue ${commit}
						cp -r $dir_head $dir_parent

						# Switch into the HEAD commit and show it.
						cd $dir_head
						git switch -d FETCH_HEAD
						echo "HEAD commit:"
						git log -1

						# Switch into the parent commit and show it.
						cd $dir_parent
						git switch -d FETCH_HEAD~1
						echo "parent commit:"
						git log -1

						cd ..
						./_scripts/runUnity.sh --skip-base $dir_parent $dir_head
						"""
				},
			]
		}
	}
}

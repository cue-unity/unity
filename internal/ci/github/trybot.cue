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

trybot: _#bashWorkflow & {

	name: "TryBot"
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
				_#step & {
					if:  "${{ \(_#isMain) }}"
					run: "echo CUE_LONG=true >> $GITHUB_ENV"
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
				_#installUnity,
				_#runUnity,
			]
		}
	}
}

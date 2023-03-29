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

workflows: daily_check: _repo.bashWorkflow & {

	name: "Daily check"
	on: {
		schedule: [{cron: "0 9 * * *"}]
	}

	jobs: {
		test: {
			steps: [
				for v in _checkoutCode {v},

				_installGo,

				// cachePre must come after installing Node and Go, because the cache locations
				// are established by running each tool.
				for v in _setupReadonlyGoActionsCaches {v},

				_installUnity,
				_runUnity,
			]
		}
	}
}

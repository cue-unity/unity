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

// package github declares the workflows for this project.
package github

import (
	"github.com/cue-unity/unity/internal/ci/base"
	"github.com/cue-unity/unity/internal/ci/gerrithub"

	"github.com/SchemaStore/schemastore/src/schemas/json"
)

workflows: [...{file: string, schema: (json.#Workflow & {})}]
workflows: [
	{
		file:   "trybot.yml"
		schema: trybot
	},
	{
		file:   "daily_check.yml"
		schema: daily_check
	},
	{
		file:   "trybot_dispatch.yml"
		schema: trybot_dispatch
	},
	{
		file:   "unity.yml"
		schema: unity
	},
	{
		file:   "unity_dispatch.yml"
		schema: unity_dispatch
	},
	{
		file:   "unity_cli_dispatch.yml"
		schema: unity_cli_dispatch
	},
]

// TODO: _#repositoryURL should be extracted from codereview.cfg
_#repositoryURL: "https://github.com/cue-unity/unity"

_#defaultBranch:     "main"
_#releaseTagPattern: "v*"

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
_#latestStableGo: "1.18.x"

_#linuxMachine:   "ubuntu-20.04"
_#macosMachine:   "macos-11"
_#windowsMachine: "windows-2022"

_#testStrategy: {
	"fail-fast": false
	matrix: {
		"go-version": [_#latestStableGo]
		os: [_#linuxMachine]
	}
}

// _gerrithub is an instance of ./gerrithub, parameterised by the properties of
// this project
_gerrithub: gerrithub & {
	#repositoryURL:                      _#repositoryURL
	#botGitHubUser:                      "porcuepine"
	#botGitHubUserTokenSecretsKey:       "PORCUEPINE_GITHUB_PAT"
	#botGitHubUserEmail:                 "porcuepine@gmail.com"
	#botGerritHubUser:                   #botGitHubUser
	#botGerritHubUserPasswordSecretsKey: "PORCUEPINE_GERRITHUB_PASSWORD"
	#botGerritHubUserEmail:              #botGitHubUserEmail
}

// _base is an instance of ./base, parameterised by the properties of this
// project
//
// TODO: revisit the naming strategy here. _base and base are very similar.
// Perhaps rename the import to something more obviously not intended to be
// used, and then rename the field base?
_base: base & {
	#repositoryURL:                "https://github.com/cue-lang/cue"
	#defaultBranch:                _#defaultBranch
	#botGitHubUser:                "porcuepine"
	#botGitHubUserTokenSecretsKey: "PORCUEPINE_GITHUB_PAT"
}

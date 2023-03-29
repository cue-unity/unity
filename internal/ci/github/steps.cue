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

_installUnity: json.#step & {
	name: "Install unity"
	run:  "./_scripts/installUnity.sh"
}

_runUnity: json.#step & {
	name: "Run unity"
	run:  "./_scripts/runUnity.sh"
}

_installGo: _repo.installGo & {
	with: "go-version": _repo.latestStableGo
}

// _setupGoActionsCaches is shared between trybot and update_tip.
_setupGoActionsCaches: _repo.setupGoActionsCaches & {
	#goVersion: _installGo.with."go-version"

	// Unfortunate that we need to hardcode here. Ideally we would be able to derive
	// the OS from the runner. i.e. from _linuxWorkflow somehow.
	#os: "Linux"

	_
}

_setupReadonlyGoActionsCaches: _setupGoActionsCaches & {
	#readonly: true
	_
}

_checkoutCode: _repo.checkoutCode & {
	#actionsCheckout: with: submodules: true
	_
}

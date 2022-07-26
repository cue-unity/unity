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

package unity

import (
	"embed"

	"github.com/cue-unity/unity/internal/unityembed"
)

//go:generate go run cuelang.org/go/cmd/cue get go --local .

// Manifest defines the schema of the manifest that a module must define to be
// added to the unity test setup
type Manifest struct {
	// Versions is a list of CUE versions that are known good to the module.
	// That is to say, running unity test with the list of versions should
	// result in success
	Versions []string
}

//go:embed *.cue
var unityFS embed.FS

func init() {
	unityembed.FS = unityFS
}
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

package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"text/template"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/load"
)

// genembed generates an embedded version of the unity CUE schema.  genembed is
// required to work around golang.org/issue/44287, i.e.  it's specifically the
// case of an unsatisfied embedding that causes module information to be
// missing. This in turn causes cue get go to output the result in the wrong
// place (despite us using --local it places the result in cue.mod/pkg). So we
// have to avoid using go:embed for now.
func main() {
	pkg := os.Getenv("GOPACKAGE")
	fn := pkg + "_genembed_gen.go"
	// Remove file in case of any issues later
	if err := os.RemoveAll(fn); err != nil {
		log.Fatal(err)
	}

	var r cue.Runtime
	bps := load.Instances([]string{"."}, nil)
	bp := bps[0]
	inst, err := r.Build(bp)
	if err != nil {
		log.Fatal(err)
	}
	byts, err := r.Marshal(inst)
	if err != nil {
		log.Fatal(err)
	}

	data := struct {
		CUEPkg       string
		Pkg          string
		InstanceData []byte
	}{
		CUEPkg:       bp.ImportPath,
		Pkg:          os.Getenv("GOPACKAGE"),
		InstanceData: byts,
	}

	tmpl := template.Must(template.New("tmp").Parse(`
package {{.Pkg}}

// {{ .Pkg }}InstanceData is the result of embedding the
// CUE instance for the {{ .CUEPkg }} package
var {{.Pkg}}InstanceData = []byte({{ printf "%+q" .InstanceData }})
`[1:]))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		log.Fatal(err)
	}
	if err := ioutil.WriteFile(fn, buf.Bytes(), 0666); err != nil {
		log.Fatal(err)
	}
}

// Code generated by cue get go. DO NOT EDIT.

//cue:generate cue get go github.com/cue-unity/unity

package unity

// Manifest defines the schema of the manifest that a module must define to be
// added to the unity test setup
#Manifest: {
	// Versions is a list of CUE versions that are known good to the module.
	// That is to say, running unity test with the list of versions should
	// result in success
	Versions: [...string] @go(,[]string)

	// GoVersion is the Go version that the module should be tested with.
	// Its format is the same as `GOVERSION`, e.g. `go1.19` or `go1.19.1`.
	// It is optional, for backwards compatibility.
	//
	// TODO(mvdan): at some point in the future, deprecate this in favor of the
	// "toolchain" line meant to be added to go.mod files.
	GoVersion?: (null | string) & =~"^go" @go(,*string)

	// GoTest is a map describing which Go tests should be run.
	// Each map key is a Go package pattern, such as `./...`.
	GoTests: {[string]: #GoTestFlags} @go(,map[string]GoTestFlags)
}

// GoTestFlags holds the flags passed to `go test`, such as `-run`.
#GoTestFlags: {
	Run: [...string] @go(,[]string)
}

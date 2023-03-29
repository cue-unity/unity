// package repo contains data values that are common to all CUE configurations
// in this repo. This not only includes GitHub workflows, but also things like
// gerrit configuration etc.
package repo

import (
	"github.com/cue-unity/unity/internal/ci/base"
)

base

githubRepositoryPath: "cue-unity/unity"

botGitHubUser:      "porcuepine"
botGitHubUserEmail: "porcuepine@gmail.com"

linuxMachine: "ubuntu-latest"

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
latestStableGo: "1.20.x"

// isLatestLinux evaluates to true if the job is running on Linux with the
// latest version of Go. This expression is often used to run certain steps
// just once per CI workflow, to avoid duplicated work.
isLatestLinux: "matrix.go-version == '\(latestStableGo)' && matrix.os == '\(linuxMachine)'"

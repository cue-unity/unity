// package repo contains data values that are common to all CUE configurations
// in this repo. This not only includes GitHub workflows, but also things like
// gerrit configuration etc.
package repo

import (
	"github.com/cue-unity/unity/internal/ci/base"
)

base

githubRepositoryPath: "cue-unity/unity"

defaultBranch: "main"

botGitHubUser:      "porcuepine"
botGitHubUserEmail: "porcuepine@gmail.com"

linuxMachine: "ubuntu-latest"

// Use the latest Go version for extra checks,
// such as running tests with the data race detector.
latestStableGo: "1.20.x"

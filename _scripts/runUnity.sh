#!/usr/bin/env bash

set -eu

# Move to the root of the repo that contains this script
cd "$( command cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )/../"
commit=$(git rev-parse HEAD)
export GOBIN=$PWD/.bin
if [ "${GITHUB_REF:-}" == "refs/heads/main" ]
then
	go install github.com/cue-lang/unity/cmd/unity@$commit
else
	go install github.com/cue-lang/unity/cmd/unity
fi

exec $GOBIN/unity test --corpus --overlay overlays --nopath "$@"

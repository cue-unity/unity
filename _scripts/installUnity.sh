#!/usr/bin/env bash

set -eu

# Move to the root of the repo that contains this script
cd "$( command cd "$( dirname "${BASH_SOURCE[0]}" )" >/dev/null 2>&1 && pwd )/../"
commit=$(git rev-parse HEAD)
export GOBIN=$PWD/.bin

# See the logic for determining PROXY_INSTALL in trybot.cue
if [ "${PROXY_INSTALL:-}" == "true" ]
then
	go install github.com/cue-unity/unity/cmd/unity@$commit
else
	go install github.com/cue-unity/unity/cmd/unity
fi

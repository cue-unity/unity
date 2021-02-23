#!/usr/bin/env bash

set -eu

# Change to the root of the repo
cd $(git rev-parse --show-toplevel)

export DOCKER_BUILDKIT=1
docker build --rm -t cueckoo/unity ./cmd/unity

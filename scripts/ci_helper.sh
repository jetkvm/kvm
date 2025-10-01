#!/bin/bash

SCRIPT_PATH=$(realpath "$(dirname $(realpath "${BASH_SOURCE[0]}"))")
source ${SCRIPT_PATH}/build_utils.sh

set -e

# check if GITHUB_ENV is set
if [ -z "$GITHUB_ENV" ]; then
    echo "GITHUB_ENV is not set"
    exit 1
fi

if [ "$1" = "prepare" ]; then
    prepare_docker_build_context
    echo "DOCKER_BUILD_CONTEXT_DIR=$DOCKER_BUILD_CONTEXT_DIR" >> $GITHUB_ENV
    echo "DOCKER_BUILD_TAG=$DOCKER_BUILD_TAG" >> $GITHUB_ENV
elif [ "$1" = "make" ]; then
    BUILD_IN_DOCKER=true
    # check if GO is available
    if ! command -v go &> /dev/null; then
        msg_info "Go is not available, will using default cache directory"
    else
        DOCKER_GO_CACHE_DIR=$(go env GOCACHE)
    fi
    do_make "${@:2}"
fi


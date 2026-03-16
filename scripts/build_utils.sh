#!/bin/bash

# check if TERM is set
# though it's not the actual way to detect if TTY is available, it's a good enough approximation for our use case
HAS_TTY=true
if [ -z "$TERM" ] || [ "$TERM" = "dumb" ]; then
    HAS_TTY=false
fi

# default colors
C_RST=$(echo -e "\e[0m")
C_ERR=$(echo -e "\e[31m")
C_OK=$(echo -e "\e[32m")
C_WARN=$(echo -e "\e[33m")
C_INFO=$(echo -e "\e[35m")

# if TTY is available, use colors
if [ "$HAS_TTY" = true ]; then
    C_RST="$(tput sgr0)"
    C_ERR="$(tput setaf 1)"
    C_OK="$(tput setaf 2)"
    C_WARN="$(tput setaf 3)"
    C_INFO="$(tput setaf 5)"
fi

msg() { printf '%s%s%s\n' $2 "$1" $C_RST; }

msg_info() { msg "$1" $C_INFO; }
msg_ok() { msg "$1" $C_OK; }
msg_err() { msg "$1" $C_ERR; }
msg_warn() { msg "$1" $C_WARN; }

DOCKER_BUILD_TAG=${DOCKER_BUILD_TAG:-ghcr.io/jetkvm/buildkit:latest}
DOCKER_BUILD_DEBUG=${DOCKER_BUILD_DEBUG:-false}
DOCKER_BUILD_CONTEXT_DIR=${DOCKER_BUILD_CONTEXT_DIR:-$(mktemp -d)}
DOCKER_GO_CACHE_DIR=${DOCKER_GO_CACHE_DIR:-$(pwd)/.cache}

BUILD_IN_DOCKER=${BUILD_IN_DOCKER:-false}

# Auto-detect container runtime: prefer docker, fall back to podman
if [ -z "$CONTAINER_CMD" ]; then
    if command -v docker &> /dev/null; then
        CONTAINER_CMD="docker"
    elif command -v podman &> /dev/null; then
        CONTAINER_CMD="podman"
    fi
fi

CONTAINER_RUN_EXTRA_ARGS=""
if [ "$CONTAINER_CMD" = "podman" ]; then
    # Podman defaults to pulling; use local image built by build_docker_image
    CONTAINER_RUN_EXTRA_ARGS="--pull=never"
fi


function prepare_docker_build_context() {
    msg_info "▶ Preparing docker build context ..."
    cp .devcontainer/install-deps.sh \
        go.mod \
        go.sum \
        Dockerfile.build \
        "${DOCKER_BUILD_CONTEXT_DIR}"

    # Podman/buildah auto-sets BUILDPLATFORM to the host arch and ignores
    # --build-arg overrides. On non-x86_64 hosts, patch the Dockerfile to
    # hardcode linux/amd64 so the correct base image is pulled.
    if [ "$CONTAINER_CMD" = "podman" ] && [ "$(uname -m)" != "x86_64" ]; then
        sed -i.bak 's/--platform=${BUILDPLATFORM}/--platform=linux\/amd64/' \
            "${DOCKER_BUILD_CONTEXT_DIR}/Dockerfile.build"
        rm -f "${DOCKER_BUILD_CONTEXT_DIR}/Dockerfile.build.bak"
    fi

    cat > "${DOCKER_BUILD_CONTEXT_DIR}/entrypoint.sh" << 'EOF'
#!/bin/bash
git config --global --add safe.directory /build
exec $@
EOF
    chmod +x "${DOCKER_BUILD_CONTEXT_DIR}/entrypoint.sh"
}

function build_docker_image() {
    if [ "$JETKVM_INSIDE_DOCKER" = 1 ]; then
        msg_err "Error: already running inside Docker"
        exit
    fi

    BUILD_ARGS=""
    # Docker needs BUILDPLATFORM as a build-arg; podman on non-x86_64 gets the
    # Dockerfile patched directly in prepare_docker_build_context instead.
    if [ "$CONTAINER_CMD" != "podman" ] || [ "$(uname -m)" = "x86_64" ]; then
        BUILD_ARGS="--build-arg BUILDPLATFORM=linux/amd64"
    fi
    if [ "$DOCKER_BUILD_DEBUG" = true ]; then
        BUILD_ARGS="$BUILD_ARGS --progress=plain --no-cache"
    fi

    msg_info "Checking if container runtime is available ..."
    if [ -z "$CONTAINER_CMD" ]; then
        msg_err "Error: Neither docker nor podman is installed"
        exit 1
    fi
    msg_info "Using container runtime: $CONTAINER_CMD"

    if [ "$CONTAINER_CMD" = "docker" ]; then
        DOCKER_BIN=$(which docker)
        if echo "$DOCKER_BIN" | grep -q "snap"; then
            msg_warn "Docker was installed using snap, this may cause issues with the build."
            msg_warn "Please consider installing Docker Engine from: https://docs.docker.com/engine/install/ubuntu/"
        fi
    fi

    prepare_docker_build_context
    pushd "${DOCKER_BUILD_CONTEXT_DIR}" > /dev/null
    msg_info "▶ Building container image ..."
    $CONTAINER_CMD build $BUILD_ARGS -t ${DOCKER_BUILD_TAG} -f Dockerfile.build .
    popd > /dev/null
}

function do_make() {
    DOCKER_BUILD_ARGS="--rm"
    if [ "$HAS_TTY" = true ]; then
        DOCKER_BUILD_ARGS="$DOCKER_BUILD_ARGS --interactive --tty"
    fi
    if [ "$BUILD_IN_DOCKER" = true ]; then
        msg_info "▶ Building the project in container ($CONTAINER_CMD) ..."
        set -x
        $CONTAINER_CMD run \
            $CONTAINER_RUN_EXTRA_ARGS \
            --env JETKVM_INSIDE_DOCKER=1 \
            -v "$(pwd):/build" \
            -v "${DOCKER_GO_CACHE_DIR}:/root/.cache/go-build" \
            ${DOCKER_BUILD_TAG} make "$@"
        set +x
    else
        msg_info "▶ Building the project in host ..."
        set -x
        make "$@"
        set +x
    fi
}

#!/bin/sh
JSON_OUTPUT=false
GET_COMMANDS=false
if [ "$1" = "-json" ]; then
    JSON_OUTPUT=true
    shift
fi
ADDITIONAL_ARGS=$@
EXIT_CODE=0

runTest() {
    PKG_ARGS=""
    if [ "$2" != "" ]; then
        PKG_ARGS="-p $2"
    fi
    if [ "$JSON_OUTPUT" = true ]; then
        ./test2json $PKG_ARGS -t $1 -test.v $ADDITIONAL_ARGS | tee $1.result.json
        if [ $? -ne 0 ]; then
            EXIT_CODE=1
        fi
    else
        $@
        if [ $? -ne 0 ]; then
            EXIT_CODE=1
        fi
    fi
}

function exit_with_code() {
    if [ $EXIT_CODE -ne 0 ]; then
        printf "\e[0;31m❌ Test failed\e[0m\n"
    fi

    exit $EXIT_CODE
}

trap exit_with_code EXIT

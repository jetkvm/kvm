#!/bin/sh
JSON_OUTPUT=false
if [ "$1" = "-json" ]; then
    JSON_OUTPUT=true
    shift
fi
ADDITIONAL_ARGS=$@
EXIT_CODE=0

runTest() {
    if [ "$JSON_OUTPUT" = true ]; then
        ./test2json $1 -test.v $ADDITIONAL_ARGS
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
        echo -e "\e[0;31m❌ Test failed\e[0m"
    else
        echo -e "\e[0;32m✅ All tests passed\e[0m"
    fi

    exit $EXIT_CODE
}

trap exit_with_code EXIT

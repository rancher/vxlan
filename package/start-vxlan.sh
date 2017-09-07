#!/bin/bash
set -e -x

trap "exit 1" SIGTERM SIGINT

if [ "${RANCHER_DEBUG}" == "true" ]; then
    DEBUG="--debug"
else
    DEBUG=""
fi

exec rancher-vxlan \
${DEBUG}

#!/bin/sh

# This script is a convenience wrapper that forwards its command-line arguments
# as the formatted input expected by the relay command on stdin.

# Use environment variable if defined, otherwise use default
host=${RELAY_HOST:-localhost}
port=${RELAY_PORT:-50135}

# Override any values from environment with command-line flags
while getopts "p:" opt; do
  case ${opt} in
    p)
      port=${OPTARG}
      ;;
    s)
      host=${OPTARG}
      ;;
    \?)
      echo "Invalid option: -${OPTARG}" >&2
      exit 1
      ;;
  esac
done

if ! nc=$( type -P nc ); then
  if ! nc=$( type -P netcat ); then
    echo "error: required command not found: nc (netcat)" >&2
    exit 1
  fi
fi

${nc} ${host} ${port} <<_EOF_
___
$*
___
_EOF_

#!/usr/bin/env bash
# Show the result of one command. The API accepting a command is not the same
# as the screen applying it: only `applied` means the target state is on screen.
#
#   ./scripts/command-get.sh 01JCOMMANDID
set -euo pipefail
# shellcheck source=scripts/_common.sh
. "$(dirname "$0")/_common.sh"

command_id="${1:?usage: command-get.sh <command-id>}"

atrium_get "/api/v1/commands/$command_id"
echo

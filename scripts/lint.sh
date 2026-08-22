#!/usr/bin/env bash
# Go linting. A missing linter is an error, not a silent skip.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

need golangci-lint "install it: https://golangci-lint.run"
( cd backend && golangci-lint run ./... "$@" )

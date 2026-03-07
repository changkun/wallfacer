#!/bin/bash
set -e

# Pass through all arguments to codex in non-interactive full-auto mode
exec codex --approval-mode full-auto "$@"

#!/bin/bash
set -e

root="$(git rev-parse --show-toplevel)"

find "$root" -type f -name '*.cs' \
  -exec sed -i 's/Pulumi.Pulumi/UnMango.Pulumi/g' {} \;

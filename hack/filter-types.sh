#!/bin/bash

root="$(git rev-parse --show-toplevel)"

jq '.components.schemas | to_entries | map(select(.key | startswith("tunnel_")))' "$root/bundled-schema.out"

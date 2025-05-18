#!/bin/bash

root="$(git rev-parse --show-toplevel)"

jq '.components.schemas | to_entries | map(select(.key == "dns-records_dns-record")) | from_entries' "$root/bundled-schema.out"

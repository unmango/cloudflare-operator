#!/usr/bin/env bash

# The kubebuilder helm plugin rewrites templates/manager/manager.yaml on every
# run and exposes no env or envFrom hook on the manager container, so the
# CLOUDFLARE_API_TOKEN stanza has to be re-inserted after each regeneration.
#
# The insert is anchored on the container's command block. It fails loudly
# rather than emitting a chart whose manager silently cannot authenticate.

set -euo pipefail

template="${1:?usage: inject-env.sh <manager template>}"

if [[ ! -f $template ]]; then
  echo "inject-env: $template does not exist" >&2
  exit 1
fi

if grep -q 'CLOUDFLARE_API_TOKEN' "$template"; then
  echo "inject-env: $template already carries the env stanza"
  exit 0
fi

anchor='^        - /bin/manager$'
matches=$(grep -c "$anchor" "$template" || true)
if [[ $matches -ne 1 ]]; then
  echo "inject-env: expected exactly one '- /bin/manager' line in $template, found $matches" >&2
  echo "inject-env: the plugin's manager template changed shape, update the anchor" >&2
  exit 1
fi

stanza=$(
  cat <<-'EOF'
		        {{- if or .Values.cloudflare.auth.apiTokenRef .Values.manager.extraEnv }}
		        env:
		        {{- with .Values.manager.extraEnv }}
		        {{- toYaml . | nindent 8 }}
		        {{- end }}
		        {{- with .Values.cloudflare.auth.apiTokenRef }}
		        - name: CLOUDFLARE_API_TOKEN
		          valueFrom:
		            secretKeyRef:
		              name: {{ required "cloudflare.auth.apiTokenRef.name is required" .name }}
		              key: {{ .key | default "CLOUDFLARE_API_TOKEN" }}
		              {{- with .optional }}
		              optional: {{ . }}
		              {{- end }}
		        {{- end }}
		        {{- end }}
	EOF
)

awk -v stanza="$stanza" '
	{ print }
	/^        - \/bin\/manager$/ { print stanza }
' "$template" >"$template.tmp"

mv "$template.tmp" "$template"
echo "inject-env: added the CLOUDFLARE_API_TOKEN stanza to $template"

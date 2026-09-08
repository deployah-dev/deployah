{{/*
DNS-1123 name: {base}-{keep} when that fits in max, otherwise
{truncatedBase}-{4hex}-{keep}. The hash is the first four hex characters
of sha256(base-keep) so two long names that share a prefix cannot collide.
The keep token always survives. Used by env ConfigMap names (max 63) and
CronJob names (max 52).
*/}}
{{- define "deployah.dns1123Name" -}}
{{- $base := .base -}}
{{- $keep := .keep -}}
{{- $max := int .max -}}
{{- $full := printf "%s-%s" $base $keep -}}
{{- if le (len $full) $max -}}
{{- $full -}}
{{- else -}}
{{- $hash := substr 0 4 (sha256sum $full) -}}
{{- $budget := int (sub $max (add (len $keep) 6)) -}}
{{- printf "%s-%s-%s" (trunc $budget $base | trimSuffix "-") $hash $keep -}}
{{- end -}}
{{- end -}}

{{/*
Runtime FileValues ConfigMap name: {fullname}-env, hashed to stay
within the 63-character DNS-1123 label limit.
*/}}
{{- define "deployah.envconfigmap.name" -}}
{{- include "deployah.dns1123Name" (dict "base" (include "common.names.fullname" .) "keep" "env" "max" 63) -}}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "deployah.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "common.names.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}


{{/*
Render container env from a string map. Values are quoted as data (no Helm tpl).
Usage:
{{ include "deployah.toEnvArray" ( dict "envVars" .Values.envVars ) }}
*/}}
{{- define "deployah.toEnvArray" -}}
{{- range $key := keys .envVars | sortAlpha }}
- name: {{ $key | quote }}
  value: {{ index $.envVars $key | quote }}
{{- end -}}
{{- end -}}

{{/*
CronJob name for a scheduled-task subchart: {release}-{task}, truncated to
52 characters with a 4-hex-char hash when needed. The API server rejects
CronJobs over 52 characters, because the controller appends an 11-character
"-$TIMESTAMP" suffix to reach the 63-character Job name limit.

The budget is spent on the release prefix so the task name always survives,
and the hash is taken over the untruncated name so two long names sharing a
prefix cannot collide. Task names are capped at 30 by the schema and Go
validation, so the prefix budget is never below 16.
*/}}
{{- define "deployah.cronjob.name" -}}
{{- include "deployah.dns1123Name" (dict "base" .Release.Name "keep" .Chart.Name "max" 52) -}}
{{- end -}}

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
Render an array of env variables. The input can be a map or a slice.
Values can be templates using the "common.tplvalues.render" helper, but changes to scope are not processed.
Usage:
{{ include "deployah.toEnvArray" ( dict "envVars" .Values.envVars "context" $ ) }}
*/}}
{{- define "deployah.toEnvArray" -}}
{{- if kindIs "map" .envVars }}
{{- range $key, $val := .envVars }}
- name: {{ $key | quote }}
{{- if kindIs "string" $val }}
  value: {{ (include "common.tplvalues.render" (dict "value" $val "context" $.context)) | quote }}
{{- else if kindIs "map" $val }}
{{ include "common.tplvalues.render" (dict "value" (omit $val "name") "context" $.context) | indent 2 }}
{{- end -}}
{{- end -}}
{{- else if kindIs "slice" .envVars }}
{{ include "common.tplvalues.render" (dict "value" .envVars "context" $.context) }}
{{- end }}
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
{{- $task := .Chart.Name -}}
{{- $full := printf "%s-%s" .Release.Name $task -}}
{{- if le (len $full) 52 -}}
{{- $full -}}
{{- else -}}
{{- $hash := substr 0 4 (sha256sum $full) -}}
{{- $budget := int (sub 52 (add (len $task) 6)) -}}
{{- printf "%s-%s-%s" (trunc $budget .Release.Name | trimSuffix "-") $hash $task -}}
{{- end -}}
{{- end -}}

{{/* Chart name, overridable. */}}
{{- define "code-search-platform.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully qualified release name. */}}
{{- define "code-search-platform.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "code-search-platform.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "code-search-platform.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: code-search-platform
{{- end -}}

{{/* The name of the Secret holding "user:password" for the Git host. */}}
{{- define "code-search-platform.credentialsSecret" -}}
{{- if .Values.indexer.credentials.existingSecret -}}
{{- .Values.indexer.credentials.existingSecret -}}
{{- else -}}
{{- printf "%s-git-credentials" (include "code-search-platform.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "code-search-platform.hasCredentials" -}}
{{- if or .Values.indexer.credentials.existingSecret .Values.indexer.credentials.create -}}true{{- end -}}
{{- end -}}

{{/* The name of the Secret holding the MCP server's bearer token. */}}
{{- define "code-search-platform.authSecret" -}}
{{- if .Values.mcp.auth.existingSecret -}}
{{- .Values.mcp.auth.existingSecret -}}
{{- else -}}
{{- printf "%s-auth" (include "code-search-platform.fullname" .) -}}
{{- end -}}
{{- end -}}

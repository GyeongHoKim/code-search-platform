{{/* Chart name, overridable. */}}
{{- define "zoekt-mcp-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/* Fully qualified release name. */}}
{{- define "zoekt-mcp-server.fullname" -}}
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

{{- define "zoekt-mcp-server.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "zoekt-mcp-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: zoekt-mcp-server
{{- end -}}

{{/* The name of the Secret holding "user:password" for the Git host. */}}
{{- define "zoekt-mcp-server.credentialsSecret" -}}
{{- if .Values.indexer.credentials.existingSecret -}}
{{- .Values.indexer.credentials.existingSecret -}}
{{- else -}}
{{- printf "%s-git-credentials" (include "zoekt-mcp-server.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "zoekt-mcp-server.hasCredentials" -}}
{{- if or .Values.indexer.credentials.existingSecret .Values.indexer.credentials.create -}}true{{- end -}}
{{- end -}}

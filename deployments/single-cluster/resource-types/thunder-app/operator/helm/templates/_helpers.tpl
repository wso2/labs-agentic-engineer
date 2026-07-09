{{/*
Common labels applied to every object this chart renders.
*/}}
{{- define "thunder-app-operator.labels" -}}
app.kubernetes.io/name: thunder-app-operator
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: aep
{{- end -}}

{{/*
Selector labels — the stable subset used by the Deployment's pod selector.
*/}}
{{- define "thunder-app-operator.selectorLabels" -}}
app.kubernetes.io/name: thunder-app-operator
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Name of the Secret holding the Thunder system client credentials: the
caller-provided existingSecret when set, otherwise the chart-managed one.
*/}}
{{- define "thunder-app-operator.credentialsSecret" -}}
{{- if .Values.thunder.existingSecret -}}
{{ .Values.thunder.existingSecret }}
{{- else -}}
thunder-app-operator-credentials
{{- end -}}
{{- end -}}

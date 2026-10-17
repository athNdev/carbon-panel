{{- define "carbon-cloud.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "carbon-cloud.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "carbon-cloud.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "carbon-cloud.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
app.kubernetes.io/name: {{ include "carbon-cloud.name" . }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
{{- end -}}

{{- define "carbon-cloud.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "carbon-cloud.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

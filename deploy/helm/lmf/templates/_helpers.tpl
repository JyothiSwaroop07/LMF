{{/*
Expand the name of the chart.
*/}}
{{- define "lmf.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "lmf.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Create chart label.
*/}}
{{- define "lmf.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels applied to all resources.
*/}}
{{- define "lmf.labels" -}}
helm.sh/chart: {{ include "lmf.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: lmf
{{- end }}

{{/*
Standard selector labels for a service component.
*/}}
{{- define "lmf.selectorLabels" -}}
app: {{ .app }}
{{- end }}

{{/*
Image reference helper.
*/}}
{{- define "lmf.image" -}}
{{ .Values.global.imageRegistry }}/{{ .name }}:{{ .Values.global.imageTag }}
{{- end }}

{{/*
Standard environment variables sourced from ConfigMap and Secrets.
*/}}
{{- define "lmf.commonEnv" -}}
- name: LOG_LEVEL
  value: {{ .Values.log.level | quote }}
- name: LOG_FORMAT
  value: {{ .Values.log.format | quote }}
- name: AMF_BASE_URL
  value: {{ .Values.externalServices.amf.baseUrl | quote }}
- name: UDM_BASE_URL
  value: {{ .Values.externalServices.udm.baseUrl | quote }}
- name: NRF_BASE_URL
  value: {{ .Values.externalServices.nrf.baseUrl | quote }}
{{- end }}

{{/*
Standard resource block.
Usage: {{ include "lmf.resources" .Values.services.sbiGateway }}
Expects: .resources.requests.cpu, .resources.requests.memory etc.
*/}}
{{- define "lmf.resources" -}}
{{- if .resources }}
resources:
  requests:
    cpu: {{ .resources.requests.cpu | quote }}
    memory: {{ .resources.requests.memory | quote }}
  limits:
    cpu: {{ .resources.limits.cpu | quote }}
    memory: {{ .resources.limits.memory | quote }}
{{- end }}
{{- end }}

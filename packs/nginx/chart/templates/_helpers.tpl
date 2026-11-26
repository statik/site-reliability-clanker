{{/* Labels of every object. */}}
{{- define "nginx.labels" -}}
{{ include "nginx.selector" . }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Selector labels. They never change between releases. */}}
{{- define "nginx.selector" -}}
app.kubernetes.io/name: nginx
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* The image, by tag and digest. */}}
{{- define "nginx.image" -}}
{{- $tag := required "image.tag is required" .Values.image.tag -}}
{{- $digest := required "image.digest is required" .Values.image.digest -}}
{{- printf "%s:%s@%s" .Values.image.repository $tag $digest -}}
{{- end -}}

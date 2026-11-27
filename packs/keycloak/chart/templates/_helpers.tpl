{{/* Labels of every object. */}}
{{- define "keycloak.labels" -}}
{{ include "keycloak.selector" . }}
app.kubernetes.io/version: {{ .Values.image.tag | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{/* Selector labels. They never change between releases. */}}
{{- define "keycloak.selector" -}}
app.kubernetes.io/name: keycloak
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/* The image, by tag and digest. The tag is the version that the collector reports. */}}
{{- define "keycloak.image" -}}
{{- $tag := required "image.tag is required" .Values.image.tag -}}
{{- $digest := required "image.digest is required" .Values.image.digest -}}
{{- printf "%s:%s@%s" .Values.image.repository $tag $digest -}}
{{- end -}}

{{/* One variable from a key of the database Secret of the platform. */}}
{{- define "keycloak.dbEnv" -}}
- name: {{ .name }}
  valueFrom:
    secretKeyRef:
      name: {{ required "platform.databaseSecret is required" .root.Values.platform.databaseSecret }}
      key: {{ .key }}
{{- end -}}

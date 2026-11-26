{{/* Image reference. The tag is the release that the platform runs, platform.version. */}}
{{- define "paperboat.image" -}}
{{- $registry := required "platform.imageRegistry is required" .Values.platform.imageRegistry -}}
{{- $tag := required "platform.version is required" .Values.platform.version -}}
{{- printf "%s/paperboat:%s" (trimSuffix "/" $registry) $tag -}}
{{- end -}}

{{/* Labels for every object. Call with (dict "root" . "component" "api"). */}}
{{- define "paperboat.labels" -}}
{{ include "paperboat.selector" . }}
app.kubernetes.io/version: {{ .root.Values.platform.version | quote }}
app.kubernetes.io/managed-by: {{ .root.Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .root.Chart.Name .root.Chart.Version }}
{{- end -}}

{{/* Selector labels. They never change between releases. */}}
{{- define "paperboat.selector" -}}
app.kubernetes.io/name: paperboat
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{/* Rolling update that never removes a ready pod before its replacement is ready. */}}
{{- define "paperboat.strategy" -}}
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxUnavailable: 0
    maxSurge: 1
{{- end -}}

{{/* Pod security settings. fsGroup lets the non-root user write to the shared volume. */}}
{{- define "paperboat.podSecurity" -}}
securityContext:
  runAsNonRoot: true
  runAsUser: 65532
  runAsGroup: 65532
  fsGroup: 65532
  seccompProfile:
    type: RuntimeDefault
{{- end -}}

{{- define "paperboat.containerSecurity" -}}
securityContext:
  allowPrivilegeEscalation: false
  readOnlyRootFilesystem: true
  capabilities:
    drop: ["ALL"]
{{- end -}}

{{- define "paperboat.resources" -}}
resources:
  requests:
    cpu: 25m
    memory: 32Mi
  limits:
    memory: 128Mi
{{- end -}}

{{- define "paperboat.databaseEnv" -}}
- name: DATABASE_URL
  valueFrom:
    secretKeyRef:
      name: {{ required "platform.databaseSecret is required" .Values.platform.databaseSecret }}
      key: DATABASE_URL
{{- end -}}

{{/* Readiness and liveness probes on /api/healthz. Call with the port name. */}}
{{- define "paperboat.probes" -}}
readinessProbe:
  httpGet:
    path: /api/healthz
    port: {{ . }}
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 2
livenessProbe:
  httpGet:
    path: /api/healthz
    port: {{ . }}
  initialDelaySeconds: 10
  periodSeconds: 10
  timeoutSeconds: 3
  failureThreshold: 6
{{- end -}}

{{- define "paperboat.sharedVolume" -}}
- name: data
  persistentVolumeClaim:
    claimName: {{ required "platform.sharedVolumeClaim is required" .Values.platform.sharedVolumeClaim }}
{{- end -}}

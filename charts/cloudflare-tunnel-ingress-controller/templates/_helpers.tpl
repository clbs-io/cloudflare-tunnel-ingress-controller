{{/*
Chart name, or nameOverride.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Name prefix of the controller's resources: fullnameOverride, or the release
name followed by the chart name unless the release name already contains it.
Truncated to 63 characters, the limit of a DNS label.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name and version for the helm.sh/chart label.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Labels of the controller's resources.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.labels" -}}
helm.sh/chart: {{ include "cloudflare-tunnel-ingress-controller.chart" . }}
{{ include "cloudflare-tunnel-ingress-controller.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels of the controller pods. A Deployment's selector is immutable,
so changing these breaks upgrades.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cloudflare-tunnel-ingress-controller.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Service account name.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.serviceAccountName" -}}
{{- include "cloudflare-tunnel-ingress-controller.fullname" . }}
{{- end }}

{{/*
Name of the cloudflared Deployment, based on the release name rather than the
full name to stay within 63 characters. For a release named cloudflare-tunnel
it would equal the legacy controller-made Deployment, which Helm cannot adopt
and whose immutable selector differs, so that release gets "-connector".
*/}}
{{- define "cloudflare-tunnel-ingress-controller.cloudflaredName" -}}
{{- $name := printf "%s-cloudflared" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- if eq $name "cloudflare-tunnel-cloudflared" }}
{{- printf "%s-connector" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name }}
{{- end }}
{{- end }}

{{/*
Name of the Secret holding the tunnel token, written by the controller.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.tunnelTokenSecretName" -}}
{{- printf "%s-tunnel-token" .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
cloudflared selector labels. The name differs from the controller's so the
selectors never overlap; fail fast when nameOverride makes the controller's
own name "cloudflared", which would collide.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.cloudflaredSelectorLabels" -}}
{{- if eq (include "cloudflare-tunnel-ingress-controller.name" .) "cloudflared" }}
{{- fail "nameOverride must not be \"cloudflared\": it would collide with the cloudflared selector labels" }}
{{- end }}
app.kubernetes.io/name: cloudflared
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
cloudflared labels.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.cloudflaredLabels" -}}
helm.sh/chart: {{ include "cloudflare-tunnel-ingress-controller.chart" . }}
{{ include "cloudflare-tunnel-ingress-controller.cloudflaredSelectorLabels" . }}
app.kubernetes.io/component: tunnel-connector
app.kubernetes.io/part-of: {{ include "cloudflare-tunnel-ingress-controller.name" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
cloudflared image; config.cloudflared.image takes precedence when set. The
image must carry an explicit tag other than latest, or a digest, so the
Deployment never changes version on its own.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.cloudflaredImage" -}}
{{- $legacy := (.Values.config).cloudflared | default dict }}
{{- $image := $legacy.image | default .Values.cloudflared.image }}
{{- if not (contains "@" $image) }}
{{- $name := last (splitList "/" $image) }}
{{- if not (contains ":" $name) }}
{{- fail "cloudflared.image must carry an explicit tag" }}
{{- end }}
{{- if eq (last (splitList ":" $name)) "latest" }}
{{- fail "cloudflared.image must not use the latest tag" }}
{{- end }}
{{- end }}
{{- $image }}
{{- end }}

{{/*
cloudflared image pull policy; config.cloudflared.imagePullPolicy takes
precedence when set.
*/}}
{{- define "cloudflare-tunnel-ingress-controller.cloudflaredImagePullPolicy" -}}
{{- $legacy := (.Values.config).cloudflared | default dict }}
{{- $legacy.imagePullPolicy | default .Values.cloudflared.imagePullPolicy }}
{{- end }}

{{/*
Expand the name of the chart.
*/}}
{{- define "temporal-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "temporal-operator.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "temporal-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "temporal-operator.labels" -}}
helm.sh/chart: {{ include "temporal-operator.chart" . }}
{{ include "temporal-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "temporal-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "temporal-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "temporal-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "temporal-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Path the watched cluster's kubeconfig is mounted at.
*/}}
{{- define "temporal-operator.kubeconfigPath" -}}
{{- printf "%s/%s" (trimSuffix "/" .Values.remote.kubeconfig.mountPath) .Values.remote.kubeconfig.key }}
{{- end }}

{{/*
Whether the operator applies the watched cluster's admission webhook configurations itself.
*/}}
{{- define "temporal-operator.bootstrapsWebhookConfiguration" -}}
{{- if and .Values.remote.enabled .Values.remote.bootstrap.enabled .Values.remote.bootstrap.webhookConfiguration -}}
true
{{- end }}
{{- end }}

{{/*
clientConfig for a single admission webhook, given a dict of "root" and "path".

An API server can only resolve a Service reference inside its own cluster, so a webhook server
reached from another cluster has to be addressed by absolute URL instead.
*/}}
{{- define "temporal-operator.webhookClientConfig" -}}
{{- $root := .root -}}
{{- if $root.Values.webhook.url -}}
url: {{ printf "%s%s" (trimSuffix "/" $root.Values.webhook.url) .path | quote }}
{{- else -}}
service:
  name: '{{ include "temporal-operator.fullname" $root }}-webhook-service'
  namespace: '{{ $root.Release.Namespace }}'
  path: {{ .path }}
{{- end }}
{{- if $root.Values.webhook.caBundle }}
caBundle: {{ $root.Values.webhook.caBundle | quote }}
{{- end }}
{{- end }}

{{/*
Annotations of an admission webhook configuration.

cert-manager's ca-injector only reconciles objects in the cluster it runs in, and it resolves the
CA of a certificate it manages. It is therefore of no use for a configuration carrying its own
bundle, nor for one addressing the webhook server by URL, which is the form used when the
configuration is applied to another cluster.
*/}}
{{- define "temporal-operator.webhookAnnotations" -}}
{{- if and (not .Values.webhook.caBundle) (not .Values.webhook.url) -}}
annotations:
  cert-manager.io/inject-ca-from: {{ .Release.Namespace }}/{{ include "temporal-operator.fullname" . }}-serving-cert
{{- end -}}
{{- end }}

{{/*
The MutatingWebhookConfiguration. Defined here rather than in its template so the same object can
be rendered into the bootstrap manifests the operator applies to the cluster it watches.

This webhook is not optional: it is the only thing that defaults a TemporalCluster spec, and an
undefaulted spec cannot be reconciled.
*/}}
{{- define "temporal-operator.mutatingWebhookConfiguration" -}}
apiVersion: admissionregistration.k8s.io/v1
kind: MutatingWebhookConfiguration
metadata:
  name: {{ include "temporal-operator.fullname" . }}-mutating-webhook-configuration
  {{- with (include "temporal-operator.webhookAnnotations" .) }}
  {{- nindent 2 . }}
  {{- end }}
  labels:
  {{- include "temporal-operator.labels" . | nindent 4 }}
webhooks:
- admissionReviewVersions:
  - v1
  clientConfig:
    {{- include "temporal-operator.webhookClientConfig" (dict "root" . "path" "/mutate-temporal-io-v1beta1-temporalcluster") | nindent 4 }}
  failurePolicy: Fail
  name: mtemporalc.kb.io
  rules:
  - apiGroups:
    - temporal.io
    apiVersions:
    - v1beta1
    operations:
    - CREATE
    - UPDATE
    resources:
    - temporalclusters
  sideEffects: None
{{- end }}

{{/*
The ValidatingWebhookConfiguration, defined here for the same reason as the mutating one.
*/}}
{{- define "temporal-operator.validatingWebhookConfiguration" -}}
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: {{ include "temporal-operator.fullname" . }}-validating-webhook-configuration
  {{- with (include "temporal-operator.webhookAnnotations" .) }}
  {{- nindent 2 . }}
  {{- end }}
  labels:
  {{- include "temporal-operator.labels" . | nindent 4 }}
webhooks:
- admissionReviewVersions:
  - v1
  clientConfig:
    {{- include "temporal-operator.webhookClientConfig" (dict "root" . "path" "/validate-temporal-io-v1beta1-temporalcluster") | nindent 4 }}
  failurePolicy: Fail
  name: vtemporalc.kb.io
  rules:
  - apiGroups:
    - temporal.io
    apiVersions:
    - v1beta1
    operations:
    - CREATE
    - UPDATE
    resources:
    - temporalclusters
  sideEffects: None
{{- end }}

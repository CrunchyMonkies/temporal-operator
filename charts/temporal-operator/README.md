# Temporal Operator Helm Chart

![Version: 0.7.0](https://img.shields.io/badge/Version-0.7.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: v0.22.0](https://img.shields.io/badge/AppVersion-v0.22.0-informational?style=flat-square)

This Helm chart deploys the Temporal Operator to manage a Temporal Cluster in a Kubernetes cluster.

## Prerequisites

- Kubernetes 1.22+
- Helm 3+

## Get repository

To add the Temporal Operator repository, use the following Helm command:

```bash
helm repo add temporal-operator https://alexandrevilain.github.io/temporal-operator
helm repo update
```

## Install Chart

To install the chart, use the following command:

```bash
helm install [RELEASE_NAME] temporal-operator/temporal-operator
```

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| imagePullSecrets | list | `[]` | Image pull secrets for accessing private image repositories. |
| installCRDs | bool | `true` | Install and manage the CustomResourceDefinitions with this chart. When true, Helm creates the CRDs on install and updates them on upgrade. The CRDs carry `helm.sh/resource-policy: keep`, so they survive `helm uninstall`. Set to false if you manage the CRDs out-of-band, and when `remote.enabled` is true, as the CRDs then belong to the watched cluster rather than this one. |
| kubernetesClusterDomain | string | `"cluster.local"` | Domain for the cluster. |
| manager.args | list | `["--leader-elect"]` | Arguments to be passed to the controller manager container. |
| manager.containerSecurityContext | object | `{"allowPrivilegeEscalation":false}` | Security context for the controller manager container. |
| manager.containerSecurityContext.allowPrivilegeEscalation | bool | `false` | Disallow privilege escalation for the container. |
| manager.env | list | `[]` | Extra environment variables for the controller manager container. |
| manager.envFrom | list | `[]` | Extra environment variable sources (configMapRef / secretRef) for the controller manager container. |
| manager.extraVolumeMounts | list | `[]` | Additional volume mounts to add to the controller manager container. |
| manager.extraVolumes | list | `[]` | Additional volumes to add to the controller manager pod. |
| manager.image.repository | string | `"ghcr.io/alexandrevilain/temporal-operator"` | Docker image repository for the controller manager container. |
| manager.leaderElectionNamespace | string | `""` | Namespace holding the leader election lease. Defaults to the namespace the operator runs in, which is only correct when the operator watches the cluster hosting it. Required when `remote.enabled` is true and `--leader-elect` is passed, because the lease is written to the watched cluster. |
| manager.nodeSelector | object | `{}` |  |
| manager.replicas | int | `1` | Number of controller manager replicas to deploy. |
| manager.resources.limits | object | `{"cpu":"500m","memory":"128Mi"}` | Resources limits for the controller manager container. |
| manager.resources.requests | object | `{"cpu":"10m","memory":"64Mi"}` | Resources requests for the controller manager container. |
| manager.serviceAccount | object | `{"annotations":{}}` | Service account settings for the controller manager container. |
| manager.tolerations | list | `[]` |  |
| remote.bootstrap.crds | bool | `true` | Include the CustomResourceDefinitions in the manifests the operator applies. |
| remote.bootstrap.enabled | bool | `false` | Let the operator apply the watched cluster's prerequisites itself on startup, instead of requiring them to be applied out of band. They are rendered into a ConfigMap and applied server-side before the controllers start. This needs the watched identity to be allowed to write CustomResourceDefinitions and admission webhook configurations. |
| remote.bootstrap.webhookConfiguration | bool | `true` | Include the admission webhook configurations in the manifests the operator applies. Requires `webhook.url`. |
| remote.bootstrapMountPath | string | `"/etc/temporal-operator/bootstrap"` | Path the bootstrap manifests ConfigMap is mounted at. |
| remote.enabled | bool | `false` | Watch a remote cluster instead of the one hosting the operator. The custom resources, CustomResourceDefinitions and admission webhook configurations then live in that cluster, not in this one, so `installCRDs` must be false. |
| remote.kubeconfig.context | string | `""` | Context to select from the kubeconfig. Defaults to its current-context. |
| remote.kubeconfig.key | string | `"kubeconfig"` | Secret key holding the kubeconfig. |
| remote.kubeconfig.mountPath | string | `"/etc/temporal-operator/kubeconfig"` | Path the kubeconfig Secret is mounted at. |
| remote.kubeconfig.secretName | string | `""` | Name of an existing Secret in the release namespace holding the kubeconfig of the cluster to watch. Required when `remote.enabled` is true. |
| targetClusters | list | `[]` | TemporalTargetCluster resources to create, one per cluster the operator should be able to create Temporal resources in. Each entry references a Secret holding that cluster's kubeconfig; the Secret itself is not managed by this chart. Only needed when the custom resources live somewhere other than the cluster Temporal runs in. For example: ```yaml targetClusters:   - name: production     kubeconfigSecretName: production-kubeconfig     key: kubeconfig     context: ""     driftDetection: Watch     resyncPeriod: "" ``` |
| webhook.caBundle | string | `""` | Base64 encoded PEM CA bundle the API server validates the webhook server with. Only needed for webhook configurations installed outside this release: when left empty and `remote.bootstrap.enabled` is true, the operator injects the CA of its own serving certificate into the configurations it applies. |
| webhook.certManager | object | `{"certificate":{"enabled":true,"extraDNSNames":[],"extraIPAddresses":[],"issuerRef":{},"useCustomIssuer":false}}` | Certificate manager settings for the webhook server. |
| webhook.certManager.certificate | object | `{"enabled":true,"extraDNSNames":[],"extraIPAddresses":[],"issuerRef":{},"useCustomIssuer":false}` | Webhook certificate configuration using cert-manager. |
| webhook.certManager.certificate.enabled | bool | `true` | Enabled defines if cert-manager should be used to manage the webhook certificate. |
| webhook.certManager.certificate.extraDNSNames | list | `[]` | Additional DNS names to add to the webhook server certificate. Set this to the host in `webhook.url` when the webhook server is reached from another cluster. |
| webhook.certManager.certificate.extraIPAddresses | list | `[]` | Additional IP addresses to add to the webhook server certificate. |
| webhook.certManager.certificate.issuerRef | object | `{}` | Issuer references if you want to use custom issuer In other case will be used selfSigned issuer. |
| webhook.certManager.certificate.useCustomIssuer | bool | `false` | Defines if cert-manager should use self-signed issuer or custom issuer. |
| webhook.containerPort | int | `9443` | The port that the webhook listens on. |
| webhook.hostNetwork | bool | `false` | Set to true if the webhook should be started in hostNetwork mode. This is useful in managed clusters (e.g. AWS EKS) with custom CNI (such as Calico), where the control-plane cannot reach pods' IP CIDR and admission webhooks are not working. `webhook.containerPort` should be adapted in case it conflicts with the host network. |
| webhook.ports | list | `[{"port":443,"protocol":"TCP","targetPort":9443}]` | Service ports settings for the webhook server. |
| webhook.type | string | `"ClusterIP"` | Service type for the webhook server. |
| webhook.url | string | `""` | Absolute URL the watched cluster's API server uses to reach the webhook server, for instance `https://temporal-operator.example.com:9443`. Required when the operator watches a remote cluster: a Service reference only resolves inside the cluster hosting the operator. The webhook server certificate must carry a matching SAN, see `webhook.certManager.certificate.extraDNSNames`. |

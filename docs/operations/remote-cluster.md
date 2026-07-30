# Managing a Temporal deployment in another cluster

By default the operator manages the cluster it runs in: it reads its custom resources from that
cluster's API server and creates Deployments, ConfigMaps and Services next to itself. Nothing on
this page is needed for that, and nothing on this page changes it.

It can also manage a Temporal deployment running somewhere else. The Temporal cluster itself always
runs in one Kubernetes cluster — the operator manages its configuration remotely, it does not
stretch a deployment across two clusters.

## Concepts

Three clusters are named on this page. They are frequently the same cluster, and were always the
same cluster before this feature existed.

| Term | Meaning |
| --- | --- |
| **management cluster** | Where the operator's own Pod runs. Chosen by wherever you install the Helm release. |
| **watched cluster** | Whose API server the operator reads custom resources from. Chosen once, by the manager's kubeconfig. |
| **target cluster** | Where a given `TemporalCluster`'s resources are created. Chosen per custom resource, by `spec.targetClusterRef`. |

One operator watches exactly one cluster. It can create resources in many target clusters.

## Two ways to place the custom resources

### Placement A — the custom resources live with Temporal

The operator watches the cluster Temporal runs in, from outside it. `TemporalCluster` resources are
applied to that cluster and need no `targetClusterRef`: the default target is the watched cluster,
which is also where they live.

```
management cluster                    watched cluster == target cluster
┌────────────────┐                    ┌──────────────────────────────┐
│ temporal-      │ ── kubeconfig ───► │ TemporalCluster              │
│ operator Pod   │                    │ Deployments, ConfigMaps, ... │
└────────────────┘                    └──────────────────────────────┘
```

Resources carry ordinary owner references and are garbage collected by that cluster, exactly as in
a single-cluster install. This is the simpler and more conservative option.

### Placement B — the custom resources live with the operator

The operator watches its own cluster, and each `TemporalCluster` names a `TemporalTargetCluster`
whose resources are created elsewhere.

```
management cluster == watched cluster              target cluster
┌────────────────────────────────┐                 ┌──────────────────────────────┐
│ temporal-operator Pod          │ ── kubeconfig ► │ Deployments, ConfigMaps, ... │
│ TemporalCluster                │                 │                              │
│ TemporalTargetCluster + Secret │                 │                              │
└────────────────────────────────┘                 └──────────────────────────────┘
```

Owner references cannot cross a cluster boundary, so managed resources are identified by labels and
cleaned up through a finalizer instead. See
[Ownership and deletion](#ownership-and-deletion-semantics), which is the part of this page most
worth reading before choosing this placement.

### Choosing

| | Placement A | Placement B |
| --- | --- | --- |
| Custom resources applied to | the cluster Temporal runs in | the management cluster |
| `spec.targetClusterRef` | not used | required |
| Admission webhooks | needed, reachable from the **watched** cluster | needed, reachable locally as usual |
| CustomResourceDefinitions installed in | the watched cluster | the management cluster |
| Ownership of managed resources | owner references | labels + finalizer |
| Number of Temporal clusters per operator | many, all in the watched cluster | many, spread over many target clusters |
| Fits | one operator per environment, resources described where they run | one operator, one place to describe every environment |

The two can be combined: an operator watching a remote cluster can also point individual
`TemporalCluster` resources at further target clusters.

## Requirements

For either placement:

- **A kubeconfig for an identity in the other cluster**, held in a Secret. Creating it is
  necessarily manual — the operator needs credentials before it can create anything. Manifests are
  below.
- **Network reachability from the operator to the other cluster's API server.**
- **The admission webhooks must be reachable from the API server that serves the custom
  resources.** They are not optional: the mutating webhook is the only thing that defaults a
  `TemporalCluster` spec, and an undefaulted spec cannot be reconciled.
- **For `TemporalNamespace` and `TemporalSchedule`, the operator must be able to dial the Temporal
  frontend.** It reads `spec.operatorClientAddress` on the `TemporalCluster`, falling back to
  in-cluster service DNS, which only resolves from inside the target cluster.

Additionally, for placement A:

- **A namespace in the watched cluster for the leader election lease**, given as
  `manager.leaderElectionNamespace`. Without it the operator derives the namespace from its own
  projected service account token and writes the lease into a namespace of the watched cluster that
  usually does not exist.
- **A routable URL for the webhook server**, given as `webhook.url`, with a matching SAN on the
  serving certificate.

## Preparing the other cluster

### An identity for the operator

Both placements need a ServiceAccount, a role, and a token turned into a kubeconfig. They differ in
what the role must allow.

Create the account and a long-lived token:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: temporal-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: temporal-operator
  namespace: temporal-system
---
apiVersion: v1
kind: Secret
metadata:
  name: temporal-operator-token
  namespace: temporal-system
  annotations:
    kubernetes.io/service-account.name: temporal-operator
type: kubernetes.io/service-account-token
```

For **placement B**, the identity only has to manage the resources the operator creates. It never
sees a custom resource:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: temporal-operator-target
rules:
- apiGroups: [""]
  resources: [configmaps, secrets, serviceaccounts, services]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [""]
  resources: [events]
  verbs: [create, get, patch]
- apiGroups: [apps]
  resources: [deployments]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [batch]
  resources: [jobs]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [networking.k8s.io]
  resources: [ingresses]
  verbs: [create, delete, get, list, update, watch]
# Only needed for the optional integrations you use.
- apiGroups: [cert-manager.io]
  resources: [certificates, issuers]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [monitoring.coreos.com]
  resources: [servicemonitors]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [networking.istio.io]
  resources: [destinationrules]
  verbs: [create, delete, get, list, update, watch]
- apiGroups: [security.istio.io]
  resources: [peerauthentications]
  verbs: [create, delete, get, list, update, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: temporal-operator-target
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: temporal-operator-target
subjects:
- kind: ServiceAccount
  name: temporal-operator
  namespace: temporal-system
```

For **placement A**, the identity additionally needs the operator's own `temporal.io` permissions
and the lease. The chart already renders exactly that role — install the chart into the *watched*
cluster with `--set manager.replicas=0`, or render the rules with:

```bash
helm template temporal-operator oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator \
  -s templates/manager-rbac.yaml -s templates/leader-election-rbac.yaml \
  --namespace temporal-system
```

and bind both to the ServiceAccount above. If you enable `remote.bootstrap`, add:

```yaml
- apiGroups: [apiextensions.k8s.io]
  resources: [customresourcedefinitions]
  verbs: [create, get, list, patch, update, watch]
- apiGroups: [admissionregistration.k8s.io]
  resources: [validatingwebhookconfigurations, mutatingwebhookconfigurations]
  verbs: [create, get, list, patch, update, watch]
```

### Turning the token into a kubeconfig Secret

Run this against the *other* cluster to build the kubeconfig, then apply the Secret to the cluster
the operator runs in:

```bash
SERVER=$(kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}')
CA=$(kubectl -n temporal-system get secret temporal-operator-token -o jsonpath='{.data.ca\.crt}')
TOKEN=$(kubectl -n temporal-system get secret temporal-operator-token -o jsonpath='{.data.token}' | base64 -d)

cat > target.kubeconfig <<EOF
apiVersion: v1
kind: Config
current-context: target
clusters:
- name: target
  cluster:
    server: ${SERVER}
    certificate-authority-data: ${CA}
contexts:
- name: target
  context:
    cluster: target
    user: temporal-operator
users:
- name: temporal-operator
  user:
    token: ${TOKEN}
EOF

kubectl --context management -n temporal-system \
  create secret generic target-kubeconfig --from-file=kubeconfig=target.kubeconfig
```

`${SERVER}` must be an address the operator's Pod can reach. With `kind`, the address in the local
kubeconfig is `127.0.0.1` and will not do; use the control plane container's address on the Docker
network instead.

The operator rebuilds its connection when the Secret's `resourceVersion` changes, so rotating the
token is a matter of updating the Secret.

## Installing the CRDs and webhook configurations in the watched cluster (placement A only)

In placement A the custom resources live in the watched cluster, so its API server is the one that
needs the CustomResourceDefinitions and the admission webhook configurations — but the Helm release
installs into the management cluster and cannot put them there. Hence `installCRDs=false` is
enforced in remote mode, and there are two ways to close the gap.

### Automatically

```
--set remote.bootstrap.enabled=true
```

The chart renders the CRDs and the webhook configurations into a ConfigMap, the operator mounts it
and applies everything server-side on startup, before its controllers start. It waits for each
CustomResourceDefinition to be established, and fills in the webhook `caBundle` from the CA of its
own serving certificate — cert-manager's `cert-manager.io/inject-ca-from` annotation cannot do that
here, because ca-injector only reconciles objects in the cluster it runs in.

This needs the two extra permission blocks listed above. Applying is idempotent: it uses server-side
apply, so restarts converge rather than conflict.

### Manually

Render the same objects and apply them yourself:

```bash
helm template temporal-operator oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator \
  --namespace temporal-system \
  --set webhook.url=https://temporal-operator.example.com:9443 \
  --set webhook.caBundle="$(kubectl --context management -n temporal-system \
      get secret webhook-server-cert -o jsonpath='{.data.ca\.crt}')" \
  -s templates/crds.yaml \
  -s templates/mutating-webhook-configuration.yaml \
  -s templates/validating-webhook-configuration.yaml \
  | kubectl --context watched apply --server-side -f -
```

Note the `webhook.caBundle`: applied by hand, the configurations have no one to inject it for them.
The CA is only available after the operator has been installed once and cert-manager has issued its
serving certificate, so with a manual prep the first install reconciles nothing until this step is
done.

## Walkthrough: placement A

The custom resources live in the cluster Temporal runs in. Requires cert-manager in the management
cluster for the webhook serving certificate, and a way for the watched cluster's API server to reach
port 9443 of the operator's Pod — an Ingress, a `LoadBalancer` Service, or a routable Pod network.

```bash
helm install temporal-operator oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator \
  --namespace temporal-system --create-namespace \
  --set manager.image.repository=ghcr.io/crunchymonkies/temporal-operator \
  --set installCRDs=false \
  --set remote.enabled=true \
  --set remote.kubeconfig.secretName=target-kubeconfig \
  --set manager.leaderElectionNamespace=temporal-system \
  --set webhook.url=https://temporal-operator.example.com:9443 \
  --set webhook.certManager.certificate.extraDNSNames[0]=temporal-operator.example.com \
  --set remote.bootstrap.enabled=true
```

Then, against the **watched** cluster:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  version: 1.24.2
  numHistoryShards: 1
  persistence:
    defaultStore:
      sql:
        user: temporal
        pluginName: postgres
        databaseName: temporal
        connectAddr: postgres.demo.svc.cluster.local:5432
      passwordSecretRef:
        name: postgres-password
        key: PASSWORD
    visibilityStore:
      sql: {}
      passwordSecretRef:
        name: postgres-password
        key: PASSWORD
```

No `targetClusterRef`: the default target is the cluster the resource came from. Everything the
operator creates is owner-referenced and garbage collected by that cluster, as usual.

## Walkthrough: placement B

The custom resources live with the operator. Install the chart normally — no `remote.*` values, the
operator watches its own cluster — then declare the target:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalTargetCluster
metadata:
  name: production
  namespace: demo
spec:
  kubeconfigSecretRef:
    name: target-kubeconfig
  key: kubeconfig
  # Open informers on the target so drift is corrected as it happens. Use Resync to poll on an
  # interval instead, when a watch connection to that cluster is undesirable.
  driftDetection: Watch
```

The Secret must be in the same namespace as the `TemporalTargetCluster`. The chart can render these
resources for you from the `targetClusters` value.

Check it before going further — an unreachable target blocks every resource pointing at it:

```console
$ kubectl get temporaltargetclusters -n demo
NAME         SERVER VERSION   READY   AGE
production   v1.31.2          True    12s
```

Then point a cluster at it:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  targetClusterRef:
    name: production
  # The operator dials the frontend to reconcile TemporalNamespaces and TemporalSchedules, and
  # cannot resolve the target cluster's internal DNS from here.
  operatorClientAddress: temporal-prod.example.com:7233
  version: 1.24.2
  numHistoryShards: 1
  # ... as above
```

`TemporalNamespace` and `TemporalSchedule` resources are applied next to the `TemporalCluster`, in
the management cluster. They take their target from the cluster they reference, so they carry no
target reference of their own.

`operatorClientAddress` only changes the address **the operator** dials. It does not appear in
Temporal's own configuration, where the frontend address stays in-cluster DNS. With mTLS the server
name expected of the frontend certificate also stays spec-derived, so SNI remains correct when the
dialled address differs.

## Ownership and deletion semantics

This section is specific to resources created in a target cluster other than the watched one —
placement B, or a watched cluster pointing at a further target.

An owner reference naming an object in another cluster is not merely useless: the target cluster's
garbage collector resolves it to nothing, concludes the resource is orphaned, and deletes it. So
resources created in a target cluster carry **no owner reference**. They are labelled instead:

```yaml
operator.temporal.io/owner-kind: TemporalCluster
operator.temporal.io/owner-name: prod
operator.temporal.io/owner-namespace: demo
operator.temporal.io/owner-uid: 4e6cf1e4-...
```

Those labels are how the operator finds what it created, and how a change in the target cluster is
mapped back to the resource that should react to it.

Because nothing in the target cluster can cascade-delete them, deletion goes through a finalizer,
`operator.temporal.io/target-cluster-cleanup`. Deleting the `TemporalCluster` makes the operator
delete every labelled resource in the target first, then drop the finalizer.

The consequence worth knowing: **if the target cluster is unreachable, deletion blocks.** The
operator refuses to drop the finalizer it cannot honour, and records a `TargetClusterCleanupBlocked`
event saying so. Force-removing the finalizer lets the custom resource go, and leaves everything it
created running in the target cluster with nothing left to clean it up.

Resources created in the watched cluster are unaffected by any of this: they keep real owner
references and real garbage collection.

## Limitations

- **One watched cluster per operator.** The kubeconfig is process-wide. To watch several clusters,
  run several releases.
- **The admission webhooks are mandatory.** A `TemporalCluster` whose spec was never defaulted
  cannot be reconciled, so an unreachable webhook server is a hard stop, not a degradation.
- **`installCRDs` must be false in remote mode**, and the watched cluster's CRDs must be kept in
  step with the operator version by bootstrap or by hand.
- **Optional APIs are resolved per target cluster.** A `TemporalCluster` asking for cert-manager
  mTLS, an Istio integration or a `ServiceMonitor` needs that API served by *its* target; the
  operator skips kinds a target does not serve, so the feature silently does not appear rather than
  failing loudly.
- **Events and status stay with the custom resource**, in the cluster it lives in. `kubectl describe`
  in the target cluster shows nothing about why a resource is as it is.

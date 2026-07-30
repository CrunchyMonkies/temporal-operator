# temporal-operator

The Kubernetes Operator to deploy and manage [Temporal](https://temporal.io/) clusters.

Using this operator, deploying a Temporal Cluster on Kubernetes is as easy as deploying the following manifest:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  version: 1.24.3
  numHistoryShards: 1
  persistence:
    defaultStore:
      sql:
        user: temporal
        pluginName: postgres
        databaseName: temporal
        connectAddr: postgres.demo.svc.cluster.local:5432
        connectProtocol: tcp
      passwordSecretRef:
        name: postgres-password
        key: PASSWORD
    visibilityStore:
      sql:
        user: temporal
        pluginName: postgres
        databaseName: temporal_visibility
        connectAddr: postgres.demo.svc.cluster.local:5432
        connectProtocol: tcp
      passwordSecretRef:
        name: postgres-password
        key: PASSWORD
```

## Documentation

The documentation is available at: [https://temporal-operator.pages.dev/](https://temporal-operator.pages.dev/).

### Quick start

To start using the Operator and deploy you first cluster in a matter of minutes, follow the documentation's [getting started guide](https://temporal-operator.pages.dev/getting-started/).

## Installation from CrunchyMonkies GitHub Packages

This fork publishes its release artifacts to its own [GitHub Packages](https://github.com/orgs/CrunchyMonkies/packages) (GHCR) instead of the upstream registry:

| Artifact         | Location                                                      |
|------------------|--------------------------------------------------------------|
| Container image  | `ghcr.io/crunchymonkies/temporal-operator`                   |
| Helm chart (OCI) | `ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator` |

The container image is tagged with the full release tag (for example `v202602.17.0`), while the Helm chart uses the SemVer form without the leading `v` (for example `202602.17.0`).

### Install with Helm

The chart is published as an OCI artifact, so no `helm repo add` is required. Install it directly from GHCR, overriding the manager image so it also points at this fork's registry:

```bash
helm install temporal-operator \
  oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator \
  --version 202602.17.0 \
  --namespace temporal-system \
  --create-namespace \
  --set manager.image.repository=ghcr.io/crunchymonkies/temporal-operator
```

To inspect the chart before installing:

```bash
helm show values oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator --version 202602.17.0
helm pull oci://ghcr.io/crunchymonkies/temporal-operator-charts/temporal-operator --version 202602.17.0
```

### CRD management

By default (`installCRDs=true`) the chart installs the Temporal CRDs and, unlike
the native Helm `crds/` directory, **updates them on `helm upgrade`** so CRD
changes ship with the release. The CRDs are annotated with
`helm.sh/resource-policy: keep`, so they are retained on `helm uninstall` (this
prevents Kubernetes from cascade-deleting your `TemporalCluster` resources). Set
`--set installCRDs=false` if you manage the CRDs out-of-band.

> **Upgrading from a chart that shipped CRDs via the `crds/` directory** (chart
> `202602.17.0` and earlier): those CRDs were not tracked by Helm, so the first
> upgrade to a chart that templates them fails with an ownership error. Adopt the
> existing CRDs once before upgrading:
> ```bash
> for c in temporalclusters temporalclusterclients temporalnamespaces temporalschedules; do
>   kubectl label   crd $c.temporal.io app.kubernetes.io/managed-by=Helm --overwrite
>   kubectl annotate crd $c.temporal.io \
>     meta.helm.sh/release-name=temporal-operator \
>     meta.helm.sh/release-namespace=temporal-system --overwrite
> done
> ```
> Fresh installs need nothing.

### Managing a Temporal deployment in another cluster

The operator can manage a Temporal deployment running in a different Kubernetes cluster, with the
custom resources living either beside that deployment or beside the operator. Requirements, the
target cluster's RBAC and kubeconfig preparation, the Helm values for each placement, and the
cross-cluster ownership and deletion semantics are documented in
[Managing a Temporal deployment in another cluster](docs/operations/remote-cluster.md).

Nothing is needed for a single-cluster install: a `TemporalCluster` that names no target cluster is
reconciled into the cluster the operator watches, as it always was.

### Pull the container image directly

```bash
docker pull ghcr.io/crunchymonkies/temporal-operator:v202602.17.0
```

> **Note:** If the packages are private, authenticate first with a GitHub token that has the `read:packages` scope:
> ```bash
> echo "$GITHUB_TOKEN" | helm registry login ghcr.io --username <your-github-user> --password-stdin
> echo "$GITHUB_TOKEN" | docker login ghcr.io --username <your-github-user> --password-stdin
> ```

## Examples

Somes examples are available to help you get started:

- [Temporal Cluster with PostgreSQL](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-postgres)
- [Temporal Cluster with MySQL](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-mysql)
- [Temporal Cluster with Cassandra](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-cassandra)
- [Temporal Cluster with PostgreSQL & advanced visibility using ElasticSearch](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-postgres-es)
- [Temporal Cluster with mTLS using cert-manager & PostgreSQL as datastore](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-mtls)
- [Temporal Cluster with mTLS using istio & PostgreSQL as datastore](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-mtls-istio)
- [Temporal Cluster with mTLS using linkerd & PostgreSQL as datastore](https://github.com/alexandrevilain/temporal-operator/blob/main/examples/cluster-mtls-linkerd)


## Compatibility matrix

The following table shows operator compatibility with Temporal and Kubernetes.
Please note this table only reports end-to-end tests suite coverage, others versions *may* work.

| Temporal Operator      | Temporal           | Kubernetes     |
|------------------------|--------------------|----------------|
| v0.22.x (not released) | v1.24.x to v1.31.x | v1.30 to v1.33 |
| v0.21.x                | v1.20.x to v1.25.x | v1.27 to v1.31 |
| v0.20.x                | v1.19.x to v1.24.x | v1.26 to v1.30 |
| v0.19.x                | v1.19.x to v1.23.x | v1.25 to v1.29 |
| v0.18.x                | v1.19.x to v1.23.x | v1.25 to v1.29 |
| v0.17.x                | v1.18.x to v1.22.x | v1.25 to v1.29 |
| v0.16.x                | v1.18.x to v1.22.x | v1.24 to v1.27 |
| v0.15.x                | v1.18.x to v1.21.x | v1.24 to v1.27 |
| v0.14.x                | v1.18.x to v1.21.x | v1.24 to v1.27 |
| v0.13.x                | v1.18.x to v1.20.x | v1.24 to v1.27 |
| v0.12.x                | v1.18.x to v1.20.x | v1.23 to v1.26 |
| v0.11.x                | v1.17.x to v1.19.x | v1.23 to v1.26 |
| v0.10.x                | v1.17.x to v1.19.x | v1.23 to v1.26 |
| v0.9.x                 | v1.16.x to v1.18.x | v1.22 to v1.25 |

## Roadmap

### Features

- [x] Deploy a new temporal cluster.
- [x] Ability to deploy multiple clusters.
- [x] Support for SQL datastores.
- [x] Deploy Web UI.
- [x] Deploy admin tools.
- [x] Support for Elastisearch.
- [x] Support for Cassandra datastore.
- [x] Automatic mTLS certificates management (using cert-manager).
- [x] Support for integration in meshes: istio & linkerd.
- [x] Namespace management using CRDs.
- [x] Cluster version upgrades.
- [x] Cluster monitoring.
- [x] Complete end2end test suite.
- [x] Archival.
- [ ] Auto scaling.
- [ ] Multi cluster replication.

## Contributing

Feel free to contribute to the project ! All issues and PRs are welcome!
To start hacking on the project, you can follow the [local development](https://temporal-operator.pages.dev/contributing/local-development/) documentation page.

## License

Temporal Operator is licensed under Apache License Version 2.0. [See LICENSE for more information](https://github.com/alexandrevilain/temporal-operator/blob/main/LICENSE).

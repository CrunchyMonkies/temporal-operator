# Admin tools

This page is WIP. Feel free to contribute [on github](https://github.com/alexandrevilain/temporal-operator/edit/main/docs/features/admin-tools.md).

## Enable admintools and set version

The admin tools image tag is shared by the admin tools deployment and by the schema setup and
update jobs, so the tools that bootstrap and upgrade the database are the ones you can run by hand.

Leave `spec.admintools.version` unset and the operator fills in the tag matching `spec.version`,
and keeps it in step across upgrades. Set it to anything else and both the deployment and the jobs
are pinned to it: bump it alongside `spec.version`, or a schema update will run with tools that
predate the server. The operator warns on create and update when the pinned tag is the default of
a different Temporal version than `spec.version`, which is what a manifest that copied the default
once and moved on looks like.

Example:
```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  version: 1.24.3
  numHistoryShards: 1
  # [...]
  admintools:
    enabled: true
    # Optional. Pins the admin tools tag for the deployment and the schema jobs.
    # Available tags: https://hub.docker.com/r/temporalio/admin-tools/tags
    version: 1.24.2-tctl-1.18.1-cli-1.0.0
```
Note: You need helm chart version 0.6.0 or above to specify admintools version.

Earlier operator releases wrote the default tag into `spec.admintools.version` on creation and never
touched it again, which froze it at the server version the cluster started with. The webhook now
refreshes such a value whenever the cluster is updated — the `spec.version` bump itself included —
so a cluster that never pinned the tag keeps following `spec.version`. A tag that is not the
operator's own default for the previous server version is left alone.
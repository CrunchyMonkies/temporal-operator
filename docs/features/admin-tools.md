# Admin tools

This page is WIP. Feel free to contribute [on github](https://github.com/alexandrevilain/temporal-operator/edit/main/docs/features/admin-tools.md).

## Enable admintools and set version

The admin tools image tag is shared by the admin tools deployment and by the schema setup and
update jobs, so the tools that bootstrap and upgrade the database are the ones you can run by hand.

Leave `spec.admintools.version` unset and the operator picks the tag matching `spec.version`, and
keeps doing so across upgrades. Set it and both the deployment and the jobs are pinned to it: bump
it alongside `spec.version`, or a schema update will run with tools that predate the server.

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

Earlier operator releases wrote the default tag into `spec.admintools.version` on creation, which
froze it at the server version the cluster started with. The webhook now clears such a value on the
cluster's next update, so a cluster that never pinned the tag goes back to following `spec.version`.
A tag you set yourself is left alone.
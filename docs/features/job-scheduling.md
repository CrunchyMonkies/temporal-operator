# Scheduling the jobs created by the operator

Beside the deployments running the temporal services, the operator creates Kubernetes Jobs to
prepare the cluster's persistence: creating the databases (or keyspaces), setting up the schemas
and updating them when `spec.version` changes.

By default those jobs' pods carry no scheduling constraints, so they land on any schedulable node.
On clusters where the nodes meant to run temporal are tainted or dedicated, this either sends the
jobs to the wrong nodes or leaves them `Pending` forever, which blocks the whole cluster
reconciliation.

`spec.jobScheduling` sets the tolerations and the affinity applied to the pods of every job the
operator creates:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  # [...]
  jobScheduling:
    tolerations:
      - key: dedicated
        operator: Equal
        value: temporal
        effect: NoSchedule
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - key: workload
                  operator: In
                  values:
                    - temporal
```

Both fields are optional and use the core Kubernetes types, so anything valid in a pod spec's
`tolerations` and `affinity` is valid here — including `podAffinity` and `podAntiAffinity`, even
though node affinity is the common case.

!!! note
    Jobs are immutable once created: changing `spec.jobScheduling` only affects the jobs the
    operator creates afterwards. Jobs which already completed are left untouched, and a job which
    is still pending has to be deleted for the new constraints to be applied.

The scheduling of the temporal services themselves is not covered by this field. Use
[overrides](overrides.md) on `spec.services` for those, and the `ui`/`admintools` overrides for the
web UI and admin tools deployments.

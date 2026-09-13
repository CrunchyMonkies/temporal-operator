# Deleting namespaces and schedules

A `TemporalNamespace` or `TemporalSchedule` with `spec.allowDeletion: true` carries the
`deletion.finalizers.temporal.io` finalizer. Deleting the custom resource then means two things: the
operator deletes the namespace (or schedule) on the Temporal server, and only afterwards drops the
finalizer so Kubernetes can remove the object.

## Deleting while the cluster is unhealthy

Deletion does not wait for the referenced `TemporalCluster` to be ready. Namespaces and their
cluster are commonly torn down together, and a namespace that waited for a cluster on its way out
would hold its finalizer forever.

What deletion does still need is an answer from the Temporal server. While the frontend is
unreachable the operator keeps retrying, and the object stays `Terminating`: the namespace may still
exist on the server, and dropping the finalizer on a guess would leave it behind with its workflows.

## When the server refuses the deletion

If the server answers the deletion with something no retry can change — a permission denied, an
invalid argument, or a server that does not implement the deletion — the operator stops retrying and
reports it. The same condition, with the same reason, is used on both resources:

```
$ kubectl get temporalnamespace demo -o jsonpath='{.status.conditions[?(@.type=="ReconcileError")]}'
{"type":"ReconcileError","status":"True","reason":"DeletionBlocked","message":"..."}
```

Fix the cause on the Temporal server, then ask for a retry. A terminating object has no spec left to
edit, so the operator does not pick the fix up on its own; changing any annotation on it re-queues a
reconciliation, which completes the deletion:

```bash
kubectl annotate temporalnamespace demo temporal.io/retry="$(date +%s)" --overwrite
kubectl annotate temporalschedule demo temporal.io/retry="$(date +%s)" --overwrite
```

A change to the referenced `TemporalCluster` re-queues its namespaces as well, and a change to a
`TemporalNamespace` re-queues its schedules, so a fix that goes through those specs needs no nudge.

## Force deletion

For a cluster that is gone for good — the frontend, its certificates, the whole datastore, or the
remote cluster it ran on — the finalizer can be dropped without contacting the server. The
annotation is checked before anything else the deletion would need, so it works even when the
`TemporalCluster` still exists but nothing behind it can be reached:

```bash
kubectl annotate temporalnamespace demo temporal.io/force-delete=true
```

The operator then removes its finalizer and the object goes away. **Whatever the resource stands for
on the Temporal server is left behind**, so use this only when there is no server left to clean up,
or when you intend to clean it up by hand. The same annotation works on a `TemporalSchedule`.

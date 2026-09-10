# Profiling with pprof

The Temporal Server ships the standard Go `net/http/pprof` endpoints. They are the usual way to
profile a live server component (heap dumps, goroutine stacks, CPU profiles) while debugging a
production memory or latency problem in the frontend, history, matching or worker.

They are **disabled by default** — pprof serves unauthenticated heap, goroutine and CPU profiles, so
it should only be switched on while investigating an issue. Enabling it is a cluster-wide setting
(`spec.pprof`), because Temporal exposes pprof through its process-wide `global.pprof` config block
and the operator renders a single server config shared by every temporal service:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  pprof:
    enabled: true     # default false
    port: 7936        # default 7936
    host: 127.0.0.1   # default 127.0.0.1
```

- **port** defaults to `7936` and must not collide with any of the services' `rpc`, `membership`,
  `http` or the `metrics` port — the webhook rejects such a configuration.
- **host** defaults to `127.0.0.1`, which keeps profiles reachable through `kubectl port-forward`
  without publishing them on the pod network. Set it to `0.0.0.0` only if you specifically want
  other pods to reach the endpoint.

When enabled, the operator adds a named `pprof` container port to every temporal service pod and
emits the `global.pprof` block into the generated server config. The port is declared but **not**
published through any Service — profiling is meant to go through a port-forward:

```bash
kubectl port-forward -n demo deploy/prod-frontend 7936:7936
# then, in another terminal
go tool pprof -seconds 30 http://127.0.0.1:7936/debug/pprof/profile
```

Profiling is opt-in per cluster; turning it on does not expose anything on the cluster network by
default and never touches authentication or TLS.
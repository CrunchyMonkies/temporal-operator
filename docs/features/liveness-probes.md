# Liveness probes

The operator sets a liveness probe on every container it deploys: the temporal services, the web
UI and the admin tools. A pod whose probe keeps failing is restarted by the kubelet, which turns a
wedged process into a recovery instead of a silent outage.

## The defaults

| Component | Probe | Rationale |
| --- | --- | --- |
| `frontend`, `history`, `matching`, `internalFrontend` | TCP connect on the `rpc` port | These services serve gRPC there; a failed connect means the process is not accepting work. |
| `worker` | TCP connect on the `membership` port | The worker serves **no** gRPC, so the `rpc` port probe the other services use would always fail. The membership ring is the cheapest signal that the worker is alive and still part of the cluster. |
| `ui` | HTTP GET `/healthz` on the `http` port | The UI answers this as soon as its HTTP server is up, without reaching out to the frontend — so a UI that can't talk to the cluster isn't restarted, only one whose HTTP server is gone. |
| `admintools` | `exec: ls /` | The admin tools image has no server to probe; this confirms the container filesystem is reachable. |

The worker's default probe is deliberately conservative — a 150 s initial delay, a 30 s period and a
failure threshold of 5, so it takes about two and a half minutes of a dead listener before the pod
is restarted. A worker that answers slowly is not a worker that needs restarting.

## Overriding a probe

Every component exposes a `livenessProbe` field accepting either a replacement probe or an explicit
disable:

```yaml
spec:
  services:
    frontend:
      livenessProbe:
        probe:
          httpGet:
            path: /health
            port: 7233
    worker:
      livenessProbe:
        probe:
          # any core/v1 Probe works here
          tcpSocket:
            port: membership
          periodSeconds: 60
    history:
      livenessProbe:
        disabled: true   # remove the generated probe entirely
  ui:
    livenessProbe:
      probe:
        httpGet:
          path: /healthz
          port: http
  adminTools:
    livenessProbe:
      disabled: true
```

Leaving the field empty keeps the operator's default. `disabled: true` removes the probe and wins
over `probe`, so the two together do not silently do nothing.

For the temporal services the field sits on each service spec (`spec.services.<name>.livenessProbe`)
and for the UI and admin tools on their own spec (`spec.ui.livenessProbe`,
`spec.adminTools.livenessProbe`).

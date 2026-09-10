# Exposing the frontend and the UI services

By default the operator creates `ClusterIP` services for the cluster's frontend and for the web UI:
they are only reachable from within the kubernetes cluster.

Using `spec.services.frontend.service` and `spec.ui.service` you can ask the operator to create
`NodePort` or `LoadBalancer` services instead, and to set custom annotations and labels on them
(most cloud load-balancer controllers are configured using service annotations).

## Exposing the frontend

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  version: 1.31.1
  numHistoryShards: 1
  # [...]
  services:
    frontend:
      service:
        type: LoadBalancer
        annotations:
          service.beta.kubernetes.io/aws-load-balancer-internal: "true"
        labels:
          exposed: "true"
```

The node port can be pinned when the service type is `NodePort` or `LoadBalancer`. For the frontend
service it applies to the gRPC port:

```yaml
spec:
  services:
    frontend:
      service:
        type: NodePort
        nodePort: 32233
```

`spec.services.frontend.service` is the only per-service `service` field which has an effect: the
history, matching and worker services are only reachable using their headless service.

## Exposing the UI

The same configuration is available for the web UI, where the node port applies to the http port:

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
  namespace: demo
spec:
  version: 1.31.1
  numHistoryShards: 1
  # [...]
  ui:
    enabled: true
    service:
      type: NodePort
      nodePort: 32080
      annotations:
        my-annotation: my-value
```

## Fields the operator doesn't manage

The operator applies a set-only merge on the service: fields which are not set in the cluster spec
keep the value they have on the live service. This means you can set fields the operator doesn't
expose (such as `spec.loadBalancerSourceRanges`, `spec.externalTrafficPolicy` or
`spec.loadBalancerClass`) directly on the created service without the reconciliation loop reverting
them at the next reconciliation.

It also means that a service type set outside of the cluster spec is kept as is. To go back to a
`ClusterIP` service, set `type: ClusterIP` explicitly instead of removing the `service` section.

Node ports allocated by the api server are preserved across reconciliations, so a service of type
`NodePort` without an explicit `nodePort` keeps the port it was allocated on creation.

## Security note

Exposing the **frontend** service using `NodePort` or `LoadBalancer` publishes the cluster's gRPC
endpoint outside of the kubernetes cluster. Traffic then reaches the frontend directly, bypassing
any mTLS-terminating ingress or service-mesh gateway deployed in front of it: the mTLS patterns
described in the [mTLS documentation](mtls/cert-manager.md) only protect the traffic that goes
through them.

Before exposing the frontend, make sure to either:

- enable mTLS on the frontend (`spec.mTLS.frontend.enabled`), so clients are authenticated by the
  frontend itself, and/or
- restrict who can reach the service at the network level, using `spec.loadBalancerSourceRanges`,
  an internal load balancer (using the annotations of your cloud provider) or network policies.

The operator returns an admission warning when the frontend service is exposed while
`spec.mTLS.frontend.enabled` is not set.

The same applies to the **UI** service: the web UI has no authentication of its own unless it's
[configured to use an OIDC provider](https://docs.temporal.io/references/web-ui-configuration).

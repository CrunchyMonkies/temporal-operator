# Archival

Temporal-operator supports configuring archival for Temporal clusters. You can get more informations about Event histories backup on [Temporal documentation](https://docs.temporal.io/clusters#archival).
The operator supports the following providers:

- AWS s3 (and any s3-compatible provider)
- Google Cloud storage
- Filestore

## Set up Archival using S3 on an Amazon EKS cluster

On EKS clusters, to connect and archive data with s3 you first need to create an IAM role with enough permissions to upload files to s3.

First, create an IAM role with the following s3 policy:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "s3:PutObjectAcl",
                "s3:PutObject",
                "s3:GetObjectVersion",
                "s3:GetObject",
                "s3:DeleteObject"
            ],
            "Resource": "arn:aws:s3:::<bucket_name>/*"
        },
        {
            "Effect": "Allow",
            "Action": "s3:ListBucket",
            "Resource": "arn:aws:s3:::<bucket_name>"
        }
    ]
}
```

Then create a trust relationships for your EKS cluster. The role is assumed by the service accounts
of the temporal services which access the bucket: the history service writes the archives, and the
frontends read them back when `enableRead` is set.

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Federated": "arn:aws:iam::<account_id>:oidc-provider/oidc.eks.<aws_region>.amazonaws.com/id/<cluster_id>"
            },
            "Action": "sts:AssumeRoleWithWebIdentity",
            "Condition": {
                "StringEquals": {
                    "oidc.eks.<aws_region>.amazonaws.com/id/<cluster_id>:sub": [
                        "system:serviceaccount:<temporal_ns>:<temporal_history_sa>",
                        "system:serviceaccount:<temporal_ns>:<temporal_frontend_sa>"
                    ]
                }
            }
        }
    ]
}
```

Then create a TemporalCluster with archival enabled and specify the role name you want to use:

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
  archival:
    enabled: true
    provider:
      s3:
        roleName: "arn:aws:iam::<account_id>:role/<aws_iam_role_id>"
        region: eu-west-1
    history:
      enabled: true
      enableRead: true
      path: "my-bucket-name"
      paused: false
    visibility:
      enabled: true
      enableRead: true
      path: "my-bucket-name2"
      paused: false
```

## Set up Archival using S3 on an Amazon EKS cluster with Pod Identity

[EKS Pod Identity](https://docs.aws.amazon.com/eks/latest/userguide/pod-identity.html) is the other
way of giving pods an IAM role on EKS. Instead of the operator annotating service accounts, you
create a Pod Identity association between the IAM role and the service accounts of the temporal
services which access the bucket — `<cluster_name>-history`, `<cluster_name>-frontend` and, if you
enabled it, `<cluster_name>-internal-frontend`. The credentials are then handed to the pods by the
EKS Pod Identity Agent, and the AWS SDK inside the temporal server picks them up from its default
credential chain.

The IAM role needs the same s3 policy as above, but its trust relationship is for the Pod Identity
service rather than the cluster OIDC provider:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Principal": {
                "Service": "pods.eks.amazonaws.com"
            },
            "Action": [
                "sts:AssumeRole",
                "sts:TagSession"
            ]
        }
    ]
}
```

Then create a TemporalCluster which asks for the default credential chain, and set no role name and
no credentials:

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
  archival:
    enabled: true
    provider:
      s3:
        useDefaultCredentials: true
        region: eu-west-1
    history:
      enabled: true
      enableRead: true
      path: "my-bucket-name"
      paused: false
    visibility:
      enabled: true
      enableRead: true
      path: "my-bucket-name2"
      paused: false
```

`useDefaultCredentials` has to be asked for explicitly: it lets the temporal services use whatever
credentials their environment provides — EKS Pod Identity, an EC2 instance profile, or anything else
injected into the pods — so the operator never falls back to it on its own. It can't be combined
with `roleName` or `credentials`, which are the two other ways of saying where the credentials come
from.

## Set up Archival using S3 on an s3-compatible object storage

If you want to archive data on an s3-compatible object storage like [OVHCloud Object storage](https://www.ovhcloud.com/en-ie/public-cloud/object-storage/) or [minio](https://min.io/) you have provide your credentials using a secret reference and then reference this secret in the TemporalCluster archival specifications. You also need to specify the s3 custom endpoint.

```bash
kubectl create secret generic archival-credentials --from-literal=AWS_ACCESS_KEY_ID=XXXX --from-literal=AWS_SECRET_ACCESS_KEY=XXXX -n demo
```

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
  archival:
    enabled: true
    provider:
      s3:
        region: gra
        endpoint: s3.gra.io.cloud.ovh.net
        credentials:
            accessKeyIdRef:
                name: archival-credentials
                key: AWS_ACCESS_KEY_ID
            secretKeyRef:
                name: archival-credentials
                key: AWS_SECRET_ACCESS_KEY
    history:
      enabled: true
      enableRead: true
      path: "dev-temporal-archival"
      paused: false
    visibility:
      enabled: true
      enableRead: true
      path: "dev-temporal-archival-visibility"
      paused: false
```

## Set up Archival using Filestore

Warning: To use your the storage you desired for filestore archival, you'll need to use overrides.

```yaml
apiVersion: temporal.io/v1beta1
kind: TemporalCluster
metadata:
  name: prod
spec:
  version: 1.24.3
  numHistoryShards: 1
  # [...]
  services:
    overrides:
      deployment:
        spec:
          template:
            spec:
              containers:
                - name: service
                  volumeMounts:
                    - name: archival-data
                      mountPath: /etc/archival
              volumes:
                - name: archival-data
                  emptyDir: {}
  archival:
    enabled: true
    provider:
      filestore: {}
    history:
      enabled: true
      enableRead: true
      path: "/etc/archival/history"
      paused: false
    visibility:
      enabled: true
      enableRead: true
      path: "/etc/archival/visibility"
      paused: false
```

## Set Up Archival using GCS

To use GCS archival you have to provide a secret containing your service account key.
To create a service account and get a key you can follow the [Google Cloud IAM documentation](https://cloud.google.com/iam/docs/keys-create-delete).

Your service account should have enough rights to write to the bucket you provide to the `TemporalCluster`'s archival spec.

Once your have downlaoded your service account key, create a secret containing this file:

```bash
kubectl create secret generic gcs-credentials --from-file=credentials.json=my-creds.json -n demo
```

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
  archival:
    enabled: true
    provider:
      gcs:
        credentialsRef:
          name: gcs-credentials
    history:
      enabled: true
      enableRead: true
      path: "temporal-operator-dev-default/temporal_archival/history"
    visibility:
      enabled: true
      enableRead: true
      path: "temporal-operator-dev-default/temporal_archival/visibility"
```

# iam-service-account-controller

Kubernetes controller that automatically manages AWS IAM roles for ServiceAccounts.

This is for EKS clusters configured for [IAM Roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

## Motivation

We want to allow users with access to a namespace to manage AWS IAM roles that can be assumed by ServiceAccounts in that namespace.

This controller creates those IAM roles transparently when they create a ServiceAccount with appropriate annotations. This way our users can manage IAM roles for ServiceAccounts in their namespace without directly accessing the AWS API.

Note that we do not allow users to directly control their role's policies like this, for security reasons.

We are using this as part of our secret management solution.

## What does this do?

If you create the following ServiceAccount (note the annotations):

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    security.kaluza.com/iam-role-managed: "true"
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/k8s-sa_bar_foo
  name: foo
  namespace: bar
```

the controller will automatically create an IAM role in the same account with an AssumeRolePolicyDocument that allows the ServiceAccount to assume the role:

```json
$ aws iam get-role --role-name k8s-sa_bar_foo
{
    "Role": {
        "Path": "/",
        "RoleName": "k8s-sa_bar_foo",
        "RoleId": "ABCDEFGHIJK1234567890",
        "Arn": "arn:aws:iam::123456789012:role/k8s-sa_bar_foo",
        "CreateDate": "2021-05-28T15:19:49+00:00",
        "AssumeRolePolicyDocument": {
            "Version": "2012-10-17",
            "Statement": [
            {
                "Effect": "Allow",
                "Principal": {
                    "Federated": "arn:aws:iam::1234567889012:oidc-provider/oidc.eks.eu-west-1.amazonaws.com/id/14758F1AFD44C09B7992073CCF00B43D"
                },
                "Action": "sts:AssumeRoleWithWebIdentity",
                "Condition": {
                    "StringEquals": {
                        "oidc.eks.eu-west-1.amazonaws.com/id/14758F1AFD44C09B7992073CCF00B43D:sub": "system:serviceaccount:bar:foo"
                }
                }
            }
            ]
        },
        "MaxSessionDuration": 3600,
        "Tags": [
            {
                "Key": "role.k8s.aws/managed-by",
                "Value": "iam-service-account-controller"
            },
            {
                "Key": "serviceaccount.k8s.aws/stack",
                "Value": "bar/foo"
            },
            {
                "Key": "role.k8s.aws/cluster",
                "Value": "cluster"
            }
        ],
        "RoleLastUsed": {}
    }
}
```

## Running locally

To run locally, ensure you have AWS creds with sufficient permissions in your environment (see permissions required in "Quick setup" section below) and:

```
$ aws eks update-kubeconfig --name $CLUSTER_NAME

$ OIDC_PROVIDER=$(aws eks describe-cluster --name $CLUSTER_NAME --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")

$ go run . -kubeconfig=$HOME/.kube/config -oidc-provider=$OIDC_PROVIDER -token-path=""
```

Note that when `-token-path` is empty the controller will use the default AWS search path for credentials instead of Web ID token authentication, which is what we want when we run locally.

## Quick setup

These instructions are for trying out the controller in your cluster. In practice you'll want set this up in a more formal manner.

We assume your EKS cluster is set up for [IAM Roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

### IAM role for the controller

We first need to create any IAM role for our controller to assume from the cluster:

```json
$ NAMESPACE=iam-service-account-controller

$ EKS_CLUSTER_NAME=cluster_name

$ ACCOUNT_ID=$(aws sts get-caller-identity | jq -r '.Account')

$ OIDC_PROVIDER=$(aws eks describe-cluster --name $EKS_CLUSTER_NAME --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")

$ cat <<EOF > /tmp/trust.json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::$OIDC_PROVIDER"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "$OIDC_PROVIDER": "system:serviceaccount:$NAMESPACE:iam-service-account-controller"
        }
      }
    }
  ]
}
EOF

$ cat <<EOF > /tmp/policy.json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "iam:CreateRole",
                "iam:DeleteRole",
                "iam:GetRole",
                "iam:TagRole"
            ],
            "Resource": "arn:aws:iam::$ACCOUNT_ID:role/k8s-sa_*"
        }
    ]
}
EOF

$ aws iam create-role \
    --role-name "iam-service-account-controller" \
    --assume-role-policy-document file:///tmp/trust.json \
    --description "IAM role for the iam-service-account k8s controller"

$ aws iam put-role-policy \
    --role-name "iam-service-account-controller" \
    --policy-name "iam-service-account-controller-policy" \
    --policy-document file:///tmp/policy.json
```

### Build and push image to repository

If you're reading this as an external party: we're not providing images. You'll want to build the image and push it to an image repository accessible to your cluster. Here we're assuming you're set up to push to a private AWS ECR repository that can be accessed by your cluster:

```
$ AWS_REGION=eu-west-1

$ REPO_NAME=iam-service-account-controller

$ GIT_TAG=$(git describe --tags --abbrev=0)

$ IMAGE_TAG=$ACCOUNT_ID.dkr.ecr.$AWS_REGION.amazonaws.com/$REPO_NAME:$GIT_TAG

$ aws ecr create-repository --repository-name $REPO_NAME

$ docker image build -t $IMAGE_TAG .

$ docker push $IMAGE_TAG
```

### Deploy controller

Finally, we can deploy the controller. Stick this in a YAML file and apply it:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: iam-service-account-controller
---
apiVersion: apps/v1
kind: Deployment
metadata:
  labels:
    app.kubernetes.io/name: iam-service-account-controller
  name: iam-service-account-controller
  namespace: iam-service-account-controller
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: iam-service-account-controller
  template:
    metadata:
      labels:
        app.kubernetes.io/name: iam-service-account-controller
    spec:
      containers:
        - name: iam-service-account-controller
          imagePullPolicy: Always
          args:
            # roles managed by this controller are prefixed with this string
            - -role-prefix=k8s-sa
            # cluster OIDC provider URL without the "https://"
            - -oidc-provider=oidc.eks.eu-west-1.amazonaws.com/id/14758F1AFD44C09B7992073CCF00B43D
            # path to the IAM web ID token for pod authentication to AWS
            - -token-path=/var/run/secrets/eks.amazonaws.com/serviceaccount/token
            # ARN of the role assumed by the controller
            - -role-arn=arn:aws:iam::123456789012:role/iam-service-account-controller
          volumeMounts:
            - mountPath: /var/run/secrets/eks.amazonaws.com/serviceaccount
              name: aws-iam-token
              readOnly: true
          image: 123456789012.dkr.ecr.eu-west-1.amazonaws.com/iam-service-account-controller:0.0.0
      serviceAccountName: iam-service-account-controller
      volumes:
        - name: aws-iam-token
          projected:
            defaultMode: 420
            sources:
              - serviceAccountToken:
                  audience: sts.amazonaws.com
                  expirationSeconds: 86400
                  path: token
---
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/iam-service-account-controller
  name: iam-service-account-controller
  namespace: iam-service-account-controller
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: iam-service-account-controller
rules:
  - apiGroups: [""]
    resources: ["serviceaccounts"]
    verbs: ["get", "watch", "list"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["create", "patch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: iam-service-account-controller
subjects:
  - kind: ServiceAccount
    name: iam-service-account-controller
    namespace: iam-service-account-controller
roleRef:
  kind: ClusterRole
  name: iam-service-account-controller
  apiGroup: rbac.authorization.k8s.io
```

### Test it

If you try to create this:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    security.kaluza.com/iam-role-managed: "true"
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/k8s-sa_default_test
  name: test
  namespace: default
```

> Note that the `eks.amazonaws.com/role-arn` value must match: `(optional-prefix_)namespace_service-account-name` - see help for more details.

you should see:

```log
$ kubectl -n iam-service-account-controller logs -f iam-service-account-controller-8595966fb5-12345
W0602 15:40:33.396159       1 client_config.go:615] Neither --kubeconfig nor --master was specified.  Using the inClusterConfig.  This might not work.
I0602 15:40:33.422062       1 controller.go:53] Creating event broadcaster
I0602 15:40:33.425272       1 controller.go:76] Setting up event handlers
I0602 15:40:33.425302       1 controller.go:98] Starting ServiceAccount controller
I0602 15:40:33.425307       1 controller.go:101] Waiting for informer caches to sync
I0602 15:40:33.527738       1 controller.go:106] Starting workers
I0602 15:40:33.527767       1 controller.go:112] Started workers
I0602 15:44:06.394955       1 controller.go:185] Syncing default/test
I0602 15:44:06.711849       1 controller.go:237] No IAM Role for 'default/test'; creating it
I0602 15:44:06.844645       1 controller.go:170] Successfully synced 'default/test'
I0602 15:44:06.844664       1 controller.go:185] Syncing default/test
I0602 15:44:06.845115       1 event.go:291] "Event occurred" object="default/test" kind="ServiceAccount" apiVersion="v1" type="Normal" reason="Synced" message="Successfully synced AWS IAM role"
I0602 15:44:06.941159       1 controller.go:170] Successfully synced 'default/test'
I0602 15:44:06.941600       1 event.go:291] "Event occurred" object="default/test" kind="ServiceAccount" apiVersion="v1" type="Normal" reason="Synced" message="Successfully synced AWS IAM role"
```

End-users can check events to help them debug:

```
$ kubectl -n default get events
LAST SEEN   TYPE      REASON            OBJECT                MESSAGE
46s         Normal    Synced            serviceaccount/test   Successfully synced AWS IAM role
```

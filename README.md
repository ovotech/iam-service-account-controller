# iam-service-account-controller

Kubernetes controller that automatically creates AWS IAM Roles for ServiceAccounts.

This is for EKS clusters configured for [IAM Roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

## Motivation

We want to allow users with access to a namespace to create AWS IAM Roles that can be assumed by their ServiceAccounts. The controller handles this transparently when they create a ServiceAccount with appropriate annotations.

Note that we don't allow users to directly control their role's policies like this, for security reasons.

We are using this as part of our secret management solution.

## What does it do?

If you create the following ServiceAccount (note the annotations):

```
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

```
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

Note that the `eks.amazonaws.com/role-arn` annotation value must match:

```
(optional-prefix_)service-account-namespace_service-account-name
```

## Running it

Not yet ready to run in cluster.

To run locally, set your Scully environment and:

```
$ aws eks update-kubeconfig --name non-production-kmi-alpha

$ OIDC_PROVIDER=$(aws eks describe-cluster --name non-production-kmi-alpha --query "cluster.identity.oidc.issuer" --output text | sed -e "s/^https:\/\///")

$ go run . -kubeconfig=$HOME/.kube/config -oidc-provider=$OIDC_PROVIDER -token-path=""
```

Note that when `-token-path` is empty the controller will use default AWS authentication, which is what we want when we run locally.

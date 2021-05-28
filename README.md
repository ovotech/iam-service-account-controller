# sa-iamrole-controller

Kubernetes controller that automatically creates AWS IAM Roles assumable by ServiceAccounts.

Requires your EKS cluster to be configured for [IAM Roles for service accounts](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html).

## Motivation

We want to allow users with access to a namespace to create AWS IAM Roles that can be assumed by their ServiceAccounts. This will do that transparently.

Note that we don't allow users to directly control their role's policies, for security reasons.

We are using this as part of our secret management solution.

## How does it work?

If you create the following ServiceAccount (note the annotation):

```
apiVersion: v1
kind: ServiceAccount
metadata:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/k8s-sa_bar_foo
  name: foo
  namespace: bar
```

the controller will automatically create an IAM Role with an AssumeRolePolicyDocument allowing that ServiceAccount to assume the role:

```
$
aws iam list-roles | jq '.Roles[] | select(.RoleName == "k8s-sa_bar_foo")'
{
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
  "MaxSessionDuration": 3600
}
```

The ServiceAccount annotation is strictly validated to ensure users can't manage an IAM Role for another namespace.

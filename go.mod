module github.com/ovotech/iam-service-account-controller

go 1.16

require (
	github.com/aws/aws-sdk-go-v2 v1.6.0
	github.com/aws/aws-sdk-go-v2/config v1.3.0
	github.com/aws/aws-sdk-go-v2/credentials v1.2.1
	github.com/aws/aws-sdk-go-v2/service/iam v1.5.0
	github.com/aws/aws-sdk-go-v2/service/sts v1.4.1
	github.com/aws/smithy-go v1.4.0
	golang.org/x/net v0.7.0 // indirect
	k8s.io/api v0.21.1
	k8s.io/apimachinery v0.21.1
	k8s.io/client-go v0.21.1
	k8s.io/klog v1.0.0
)

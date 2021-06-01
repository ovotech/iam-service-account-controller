package iam

import (
	"context"
	"errors"
	"fmt"
	"log"

	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awstypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	awssts "github.com/aws/aws-sdk-go-v2/service/sts"
	"k8s.io/klog"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	iamerrors "github.com/ovotech/sa-iamrole-controller/pkg/iam/errors"
	"github.com/ovotech/sa-iamrole-controller/pkg/ref"
)

const (
	clusterTagKey     = "role.k8s.aws/cluster"
	managedByTagKey   = "role.k8s.aws/managed-by"
	managedByTagValue = "sa-iamrole-controller"
	stackTagKey       = "serviceaccount.k8s.aws/stack"
)

type Manager struct {
	client       *iam.Client
	rolePrefix   string
	accountId    string
	oidcProvider string
	clusterName  string
}

func NewManager(
	rolePrefix string,
	region string,
	oidcProvider string,
	clusterName string,
) *Manager {
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		log.Fatalf("Unable to load AWS SDK config: %v", err)
	}

	stsClient := awssts.NewFromConfig(cfg)
	callerIdentity, err := stsClient.GetCallerIdentity(
		context.TODO(),
		&awssts.GetCallerIdentityInput{},
	)
	if err != nil {
		klog.Fatalf("Unable to get account identifer from AWS STS: %v", err)
	}

	return &Manager{
		client:       awsiam.NewFromConfig(cfg),
		rolePrefix:   rolePrefix,
		accountId:    *callerIdentity.Account,
		oidcProvider: oidcProvider,
		clusterName:  clusterName,
	}
}

// makeRoleFQN returns the fully qualified name for the role. This is a string with the format:
// (prefix_)namespace_name
func (m *Manager) makeRoleFQN(name string, namespace string) string {
	if m.rolePrefix == "" {
		return fmt.Sprintf("%s_%s", namespace, name)
	}
	return fmt.Sprintf("%s_%s_%s", m.rolePrefix, namespace, name)
}

// makeAccessPolicy returns a string of an IAM Access Policy that allows AssumeRoleWithWebIdentity
// for the k8s ServiceAccount with given namespace/name.
func (m *Manager) makeAccessPolicy(name string, namespace string) string {
	return fmt.Sprintf(`{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::%s:oidc-provider/%s"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "%s:sub": "system:serviceaccount:%s:%s"
        }
      }
    }
  ]
}`, m.accountId, m.oidcProvider, m.oidcProvider, namespace, name)
}

// MakeRoleARN returns the AWS ARN for a role given the k8s ServieAccount namespace/name. Note that
// this is an ARN generated locally from the name and namespace strings and is not an ARN looked up
// on AWS. As such this role may or may not exist in AWS.
func (m *Manager) MakeRoleARN(name string, namespace string) string {
	fqn := m.makeRoleFQN(name, namespace)
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", m.accountId, fqn)
}

// GetRole will fetch the AWS IAM Role for the k8s ServiceAccount namespace/name.
func (m *Manager) GetRole(name string, namespace string) (*awsiamtypes.Role, error) {
	fqn := m.makeRoleFQN(name, namespace)

	roleOutput, err := m.client.GetRole(context.TODO(), &iam.GetRoleInput{RoleName: &fqn})
	if err != nil {
		var ae smithy.APIError
		if errors.As(err, &ae) && ae.ErrorCode() == "NoSuchEntity" {
			return nil, &iamerrors.IAMError{
				Code:    iamerrors.NotFoundErrorCode,
				Message: ae.ErrorMessage(),
			}
		}
		return nil, &iamerrors.IAMError{Code: iamerrors.OtherErrorCode, Message: err.Error()}
	}

	return roleOutput.Role, nil
}

// CreateRole will create an AWS IAM Role for the k8s ServiceAccount namespace/name.
func (m *Manager) CreateRole(name string, namespace string) error {
	fqn := m.makeRoleFQN(name, namespace)
	accessPolicy := m.makeAccessPolicy(name, namespace)
	stackTagValue := fmt.Sprintf("%s/%s", namespace, name)
	tags := []awstypes.Tag{
		{Key: ref.String(managedByTagKey), Value: ref.String(managedByTagValue)},
		{Key: ref.String(stackTagKey), Value: &stackTagValue},
		{Key: ref.String(clusterTagKey), Value: &m.clusterName},
	}

	_, err := m.client.CreateRole(
		context.TODO(),
		&iam.CreateRoleInput{AssumeRolePolicyDocument: &accessPolicy, RoleName: &fqn, Tags: tags},
	)
	if err != nil {
		return &iamerrors.IAMError{Code: iamerrors.OtherErrorCode, Message: err.Error()}
	}

	return nil
}

// DeleteRole will delete an AWS IAM Role for the k8s ServiceAccount namespace/name if it the Role
// exists and it's managed by this controller.
func (m *Manager) DeleteRole(name string, namespace string) error {
	managed, err := m.isManaged(name, namespace)
	if err != nil {
		return err
	}
	if !managed {
		return nil
	}

	fqn := m.makeRoleFQN(name, namespace)

	_, err = m.client.DeleteRole(context.TODO(), &iam.DeleteRoleInput{RoleName: &fqn})
	if err != nil {
		return &iamerrors.IAMError{Code: iamerrors.OtherErrorCode, Message: err.Error()}
	}

	return nil
}

// isManaged checks if an AWS IAM Role for the ServiceAccount namespace/name is managed by this
// controller. This check is based on AWS tags.
func (m *Manager) isManaged(name string, namespace string) (bool, error) {
	// TODO check tags
	_, err := m.GetRole(name, namespace)
	if err != nil {
		if iamerrors.IsNotFound(err) {
			return false, nil
		} else {
			return false, err
		}
	}
	return true, err
}

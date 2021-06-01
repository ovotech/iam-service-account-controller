package iam

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
	awstypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	awssts "github.com/aws/aws-sdk-go-v2/service/sts"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	awsiamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
	"github.com/aws/smithy-go"
	iamerrors "github.com/ovotech/iam-service-account-controller/pkg/iam/errors"
	"github.com/ovotech/iam-service-account-controller/pkg/ref"
)

const (
	clusterTagKey   = "role.k8s.aws/cluster"
	managedByTagKey = "role.k8s.aws/managed-by"
	stackTagKey     = "serviceaccount.k8s.aws/stack"
)

type Manager struct {
	client         *iam.Client
	rolePrefix     string
	accountId      string
	oidcProvider   string
	clusterName    string
	controllerName string
}

func NewManagerWithDefaultConfig(
	controllerName string,
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
		log.Fatalf("Unable to get account identifer from AWS STS: %v", err)
	}

	return &Manager{
		client:         awsiam.NewFromConfig(cfg),
		rolePrefix:     rolePrefix,
		accountId:      *callerIdentity.Account,
		oidcProvider:   oidcProvider,
		clusterName:    clusterName,
		controllerName: controllerName,
	}
}

func NewManagerWithWebIdToken(
	controllerName string,
	rolePrefix string,
	region string,
	oidcProvider string,
	clusterName string,
	controllerRole string,
	tokenPath string,
) *Manager {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
	)
	if err != nil {
		log.Fatalf("Unable to load AWS SDK config: %v", err)
	}

	stsClient := awssts.NewFromConfig(cfg)
	callerIdentity, err := stsClient.GetCallerIdentity(
		context.TODO(),
		&awssts.GetCallerIdentityInput{},
	)
	if err != nil {
		log.Fatalf("Unable to get account identifer from AWS STS: %v", err)
	}

	accountId := *callerIdentity.Account
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountId, controllerRole)
	appCreds := aws.NewCredentialsCache(
		stscreds.NewWebIdentityRoleProvider(
			stsClient,
			roleARN,
			stscreds.IdentityTokenFile(tokenPath),
			func(o *stscreds.WebIdentityRoleOptions) {
				o.RoleSessionName = controllerName
			},
		),
	)

	iamClient := awsiam.NewFromConfig(cfg, func(o *awsiam.Options) {
		o.Credentials = appCreds
	})

	return &Manager{
		client:         iamClient,
		rolePrefix:     rolePrefix,
		accountId:      accountId,
		oidcProvider:   oidcProvider,
		clusterName:    clusterName,
		controllerName: controllerName,
	}
}

// makeIAMRoleName returns the fully qualified name for the role. This is a string with the format:
// (prefix_)namespace_name
func (m *Manager) makeIAMRoleName(name string, namespace string) string {
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
	roleName := m.makeIAMRoleName(name, namespace)
	return fmt.Sprintf("arn:aws:iam::%s:role/%s", m.accountId, roleName)
}

// GetRole will fetch the AWS IAM Role for the k8s ServiceAccount namespace/name.
func (m *Manager) GetRole(name string, namespace string) (*awsiamtypes.Role, error) {
	roleName := m.makeIAMRoleName(name, namespace)

	roleOutput, err := m.client.GetRole(context.TODO(), &iam.GetRoleInput{RoleName: &roleName})
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
	roleName := m.makeIAMRoleName(name, namespace)
	accessPolicy := m.makeAccessPolicy(name, namespace)
	stackTagValue := fmt.Sprintf("%s/%s", namespace, name)
	tags := []awstypes.Tag{
		{Key: ref.String(managedByTagKey), Value: ref.String(m.controllerName)},
		{Key: ref.String(stackTagKey), Value: &stackTagValue},
		{Key: ref.String(clusterTagKey), Value: &m.clusterName},
	}

	_, err := m.client.CreateRole(
		context.TODO(),
		&iam.CreateRoleInput{
			AssumeRolePolicyDocument: &accessPolicy,
			RoleName:                 &roleName,
			Tags:                     tags,
		},
	)
	if err != nil {
		return &iamerrors.IAMError{Code: iamerrors.OtherErrorCode, Message: err.Error()}
	}

	return nil
}

// DeleteRole will delete an AWS IAM Role for the k8s ServiceAccount namespace/name if it the Role
// exists and it's managed by this controller.
func (m *Manager) DeleteRole(name string, namespace string) error {
	role, err := m.GetRole(name, namespace)
	if err != nil {
		// if there is no role, nothing to do and this is not an error
		if iamerrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	if !m.IsManaged(role) {
		return &iamerrors.IAMError{
			Code:    iamerrors.NotManagedErrorCode,
			Message: "Role not managed by controller",
		}
	}

	roleName := m.makeIAMRoleName(name, namespace)

	_, err = m.client.DeleteRole(context.TODO(), &iam.DeleteRoleInput{RoleName: &roleName})
	if err != nil {
		return &iamerrors.IAMError{Code: iamerrors.OtherErrorCode, Message: err.Error()}
	}

	return nil
}

// isManaged checks if an AWS IAM Role for the ServiceAccount namespace/name is managed by this
// controller. This check is based on AWS tags.
func (m *Manager) IsManaged(role *awsiamtypes.Role) bool {
	for _, tag := range role.Tags {
		if *tag.Key == managedByTagKey && *tag.Value == m.controllerName {
			return true
		}
	}

	return false
}

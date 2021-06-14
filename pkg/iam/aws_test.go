package iam

import (
	"context"
	"fmt"
	"testing"

	awsiam "github.com/aws/aws-sdk-go-v2/service/iam"
)

func TestMakeIAMRoleName(t *testing.T) {
	var tests = []struct {
		name      string
		namespace string
		prefix    string
		want      string
	}{
		{"test", "default", "k8s-sa", "k8s-sa_default_test"},
		{"test", "default", "", "default_test"},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf("%s,%s,%s,%s", tt.name, tt.namespace, tt.prefix, tt.want)
		m := Manager{
			client:         awsiam.New(awsiam.Options{}),
			rolePrefix:     tt.prefix,
			accountId:      "123456789012",
			oidcProvider:   "https://cognito-idp.eu-west-1.amazonaws.com/eu-west-1_ABCD",
			clusterName:    "cluster",
			controllerName: "iam-service-account-controller",
			ctx:            context.TODO(),
		}
		t.Run(testname, func(t *testing.T) {
			ans := m.makeIAMRoleName(tt.name, tt.namespace)
			if ans != tt.want {
				t.Errorf("got %s, want %s", ans, tt.want)
			}
		})
	}
}

func TestMakeRoleARN(t *testing.T) {
	var tests = []struct {
		name      string
		namespace string
		prefix    string
		accountId string
		want      string
	}{
		{
			"test",
			"default",
			"k8s-sa",
			"123456789012",
			"arn:aws:iam::123456789012:role/k8s-sa_default_test",
		},
		{
			"test",
			"default",
			"",
			"123456789012",
			"arn:aws:iam::123456789012:role/default_test",
		},
	}

	for _, tt := range tests {
		testname := fmt.Sprintf(
			"%s,%s,%s,%s,%s",
			tt.name,
			tt.namespace,
			tt.prefix,
			tt.accountId,
			tt.want,
		)
		m := Manager{
			client:         awsiam.New(awsiam.Options{}),
			rolePrefix:     tt.prefix,
			accountId:      tt.accountId,
			oidcProvider:   "https://cognito-idp.eu-west-1.amazonaws.com/eu-west-1_ABCD",
			clusterName:    "cluster",
			controllerName: "iam-service-account-controller",
			ctx:            context.TODO(),
		}
		t.Run(testname, func(t *testing.T) {
			ans := m.MakeRoleARN(tt.name, tt.namespace)
			if ans != tt.want {
				t.Errorf("got %s, want %s", ans, tt.want)
			}
		})
	}
}

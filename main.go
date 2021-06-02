package main

import (
	"flag"
	"time"

	kubeinformers "k8s.io/client-go/informers"

	"github.com/ovotech/iam-service-account-controller/pkg/iam"
	"github.com/ovotech/iam-service-account-controller/pkg/signals"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

const (
	controllerName = "iam-service-account-controller"
)

var (
	masterURL                string
	kubeconfig               string
	syncInterval             time.Duration
	workerThreads            int
	awsRegion                string
	iamRolePrefix            string
	oidcProvider             string
	clusterName              string
	controllerIAMRoleARN     string
	controllerWebIdTokenPath string
)

func main() {
	flag.Parse()
	stopCh := signals.SetupSignalHandler()

	if oidcProvider == "" {
		klog.Fatalf(
			"Invalid OIDC provider: '%s'. See help for more information.",
			oidcProvider,
		)
	}

	var iamManager *iam.Manager
	if controllerWebIdTokenPath == "" {
		iamManager = iam.NewManagerWithDefaultConfig(
			controllerName,
			iamRolePrefix,
			awsRegion,
			oidcProvider,
			clusterName,
		)
	} else {
		// ARN is required for web id token auth
		if controllerIAMRoleARN == "" {
			klog.Fatalf(
				"Invalid role ARN for controller when using web ID token auth: '%s'. See help for more information.",
				controllerIAMRoleARN,
			)
		}
		iamManager = iam.NewManagerWithWebIdToken(
			controllerName,
			iamRolePrefix,
			awsRegion,
			oidcProvider,
			clusterName,
			controllerIAMRoleARN,
			controllerWebIdTokenPath,
		)
	}

	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, syncInterval)
	controller := NewController(
		kubeClient,
		kubeInformerFactory.Core().V1().ServiceAccounts(),
		iamManager,
	)
	kubeInformerFactory.Start(stopCh)

	if err = controller.Run(workerThreads, stopCh); err != nil {
		klog.Fatalf("Error running controller: %s", err.Error())
	}
}

func init() {
	flag.StringVar(
		&kubeconfig,
		"kubeconfig",
		"",
		"Path to a kubeconfig. Only required if out-of-cluster.",
	)
	flag.StringVar(
		&masterURL,
		"master",
		"",
		"The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.",
	)
	flag.DurationVar(
		&syncInterval,
		"sync-interval",
		time.Minute*5,
		"The interval between ServiceAccount syncs.",
	)
	flag.IntVar(
		&workerThreads,
		"worker-threads",
		2,
		"The number of worker threads processing ServiceAccount events.",
	)
	flag.StringVar(
		&awsRegion,
		"region",
		"eu-west-1",
		"The AWS region for AWS IAM Role management.",
	)
	flag.StringVar(
		&iamRolePrefix,
		"role-prefix",
		"k8s-sa",
		"The AWS IAM roles managed by the controller have this string prefixed to their names.",
	)
	flag.StringVar(
		&controllerIAMRoleARN,
		"role-arn",
		"",
		"The full ARN of the AWS IAM role used by the controller.",
	)
	flag.StringVar(
		&controllerWebIdTokenPath,
		"token-path",
		"/var/run/secrets/eks.amazonaws.com/serviceaccount/token",
		"Path to the AWS Web Identity Token in the pod. If empty will use default authentication instead (i.e. useful if running locally).",
	)
	flag.StringVar(
		&oidcProvider,
		"oidc-provider",
		"",
		"This should be the OIDC provider, for example: 'oidc.eks.eu-west-1.amazonaws.com/id/14758F1AFD44C09B7992073CCF00B43D'. You can get this with 'aws eks describe-cluster --name <cluster_name> --query \"cluster.identity.oidc.issuer\" --output text | sed -e \"s/^https:\\/\\///\"'.",
	)
	flag.StringVar(
		&clusterName,
		"cluster-name",
		"cluster",
		"Name of the cluster.",
	)
}

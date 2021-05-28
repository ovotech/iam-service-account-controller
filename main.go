package main

import (
	"flag"
	"time"

	kubeinformers "k8s.io/client-go/informers"

	"github.com/ovotech/sa-iamrole-controller/pkg/iam"
	"github.com/ovotech/sa-iamrole-controller/pkg/signals"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	masterURL     string
	kubeconfig    string
	syncInterval  time.Duration
	workerThreads int
	awsRegion     string
	iamRolePrefix string
	oidcProvider  string
	clusterName   string
)

func main() {
	flag.Parse()
	stopCh := signals.SetupSignalHandler()

	if oidcProvider == "" {
		klog.Fatalf(
			"Invalid OIDC issuer URL: '%s'. See help for more information.",
			oidcProvider,
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
		iam.NewManager(iamRolePrefix, awsRegion, oidcProvider, clusterName),
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
		"aws-region",
		"eu-west-1",
		"The AWS region for AWS IAM Role management.",
	)
	flag.StringVar(
		&iamRolePrefix,
		"iam-role-prefix",
		"k8s-sa",
		"Prefix for the IAM Role name.",
	)
	flag.StringVar(
		&oidcProvider,
		"oidc-issuer-url",
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

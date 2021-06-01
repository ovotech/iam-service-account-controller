package main

import (
	"fmt"
	"time"

	"github.com/ovotech/sa-iamrole-controller/pkg/iam"
	iamerrors "github.com/ovotech/sa-iamrole-controller/pkg/iam/errors"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"
)

const (
	controllerAgentName       = "sa-iamrole-controller"
	managedAnnotationKey      = "iamrole/managed"
	roleAnnotationKey         = "eks.amazonaws.com/role-arn"
	SyncSuccess               = "Synced"
	MessageResourceSynced     = "Successfully synced with AWS IAM role"
	SyncFailed                = "SyncFailed"
	MessageRoleCreationFailed = "Failed to create AWS IAM role due to: %s"
	SyncWarning               = "SyncWarning"
	MessageUnmanagedRole      = "AWS IAM role exists but is not managed by controller"
)

type Controller struct {
	kubeclientset         kubernetes.Interface
	serviceAccountsLister corelisters.ServiceAccountLister
	serviceAccountsSynced cache.InformerSynced
	workqueue             workqueue.RateLimitingInterface
	recorder              record.EventRecorder
	iam                   *iam.Manager
}

func NewController(
	kubeclientset kubernetes.Interface,
	serviceAccountInformer coreinformers.ServiceAccountInformer,
	iamManager *iam.Manager) *Controller {

	klog.Info("Creating event broadcaster")
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartStructuredLogging(0)
	eventBroadcaster.StartRecordingToSink(
		&typedcorev1.EventSinkImpl{Interface: kubeclientset.CoreV1().Events("")},
	)
	recorder := eventBroadcaster.NewRecorder(
		scheme.Scheme,
		corev1.EventSource{Component: controllerAgentName},
	)

	controller := &Controller{
		kubeclientset:         kubeclientset,
		serviceAccountsLister: serviceAccountInformer.Lister(),
		serviceAccountsSynced: serviceAccountInformer.Informer().HasSynced,
		workqueue: workqueue.NewNamedRateLimitingQueue(
			workqueue.DefaultControllerRateLimiter(),
			"ServiceAccounts",
		),
		recorder: recorder,
		iam:      iamManager,
	}

	klog.Info("Setting up event handlers")

	serviceAccountInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.enqueueServiceAccount,
		DeleteFunc: controller.enqueueServiceAccount,
	})

	return controller
}

// Run will set up the event handlers for types we are interested in, as well
// as syncing informer caches and starting workers. It will block until stopCh
// is closed, at which point it will shutdown the workqueue and wait for
// workers to finish processing their current work items.
func (c *Controller) Run(threadiness int, stopCh <-chan struct{}) error {
	defer utilruntime.HandleCrash()
	defer c.workqueue.ShutDown()

	// Start the informer factories to begin populating the informer caches
	klog.Info("Starting ServiceAccount controller")

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.serviceAccountsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync")
	}

	klog.Info("Starting workers")
	// Launch workers to process ServiceAccount resources
	for i := 0; i < threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, stopCh)
	}

	klog.Info("Started workers")
	<-stopCh
	klog.Info("Shutting down workers")

	return nil
}

// runWorker is a long-running function that will continually call the
// processNextWorkItem function in order to read and process a message on the
// workqueue.
func (c *Controller) runWorker() {
	for c.processNextWorkItem() {
	}
}

// processNextWorkItem will read a single work item off the workqueue and
// attempt to process it, by calling the syncHandler.
func (c *Controller) processNextWorkItem() bool {
	obj, shutdown := c.workqueue.Get()

	if shutdown {
		return false
	}

	// We wrap this block in a func so we can defer c.workqueue.Done.
	err := func(obj interface{}) error {
		// We call Done here so the workqueue knows we have finished
		// processing this item. We also must remember to call Forget if we
		// do not want this work item being re-queued. For example, we do
		// not call Forget if a transient error occurs, instead the item is
		// put/ back on the workqueue and attempted again after a back-off
		// period.
		defer c.workqueue.Done(obj)
		var key string
		var ok bool
		// We expect strings to come off the workqueue. These are of the
		// form namespace/name. We do this as the delayed nature of the
		// workqueue means the items in the informer cache may actually be
		// more up to date that when the item was initially put onto the
		// workqueue.
		if key, ok = obj.(string); !ok {
			// As the item in the workqueue is actually invalid, we call
			// Forget here else we'd go into a loop of attempting to
			// process a work item that is invalid.
			c.workqueue.Forget(obj)
			utilruntime.HandleError(fmt.Errorf("expected string in workqueue but got %#v", obj))
			return nil
		}
		// Run the syncHandler, passing it the namespace/name string of the
		// ServiceAccount resource to be synced.
		if err := c.syncHandler(key); err != nil {
			// Put the item back on the workqueue to handle any transient errors.
			c.workqueue.AddRateLimited(key)
			return fmt.Errorf("error syncing '%s': %s, requeuing", key, err.Error())
		}
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		c.workqueue.Forget(obj)
		klog.Infof("Successfully synced '%s'", key)
		return nil
	}(obj)

	if err != nil {
		utilruntime.HandleError(err)
		return true
	}

	return true
}

// syncHandler compares the actual state with the desired, and attempts to
// converge the two.
func (c *Controller) syncHandler(serviceAccountKey string) error {
	klog.Infof("Syncing %s\n", serviceAccountKey)

	// Convert the namespace/name string into a distinct namespace and name
	namespace, name, err := cache.SplitMetaNamespaceKey(serviceAccountKey)
	if err != nil {
		utilruntime.HandleError(fmt.Errorf("Invalid resource key: %s", serviceAccountKey))
		return nil
	}

	// Get the ServiceAccount resource with this namespace/name.
	sa, err := c.serviceAccountsLister.ServiceAccounts(namespace).Get(name)
	if err != nil {
		// The ServiceAccount no longer exists (i.e. it's been deleted from the cluster).
		// We ensure its IAM Role is removed from AWS.
		if k8serrors.IsNotFound(err) {
			klog.Infof(
				"ServiceAccount '%s' no longer exists, will delete its IAM Role",
				serviceAccountKey,
			)
			if err := c.iam.DeleteRole(name, namespace); err != nil {
				return err
			}
			return nil
		}
		// Requeue to try again
		return err
	}

	// We try to fetch the role from AWS. If it doesn't exist we create it.
	role, err := c.iam.GetRole(name, namespace)
	if err != nil {
		if iamerrors.IsNotFound(err) {
			klog.Infof("No IAM Role for '%s'; creating it", serviceAccountKey)
			if err := c.iam.CreateRole(name, namespace); err != nil {
				// Failed to create the role for some reason
				// We log an error event and requeue
				c.recorder.Event(
					sa,
					corev1.EventTypeWarning,
					SyncFailed,
					fmt.Sprintf(MessageRoleCreationFailed, err.Error()),
				)
				return err
			}
			c.recorder.Event(sa, corev1.EventTypeNormal, SyncSuccess, MessageResourceSynced)
			return nil
		} else {
			// Some other error we can't handle now, requeue
			return err
		}
	}

	// The role already exists, check if it's managed by us
	if c.iam.IsManaged(role) {
		c.recorder.Event(sa, corev1.EventTypeNormal, SyncSuccess, MessageResourceSynced)
	} else {
		c.recorder.Event(sa, corev1.EventTypeWarning, SyncWarning, MessageUnmanagedRole)
	}

	return nil
}

// enqueueServiceAccount takes a ServiceAccount resource and converts it into a namespace/name
// string which is then put onto the work queue. It first checks the ServiceAccount's annotations to
// see if this SA should be managed by this controller.
func (c *Controller) enqueueServiceAccount(obj interface{}) {
	var sa *corev1.ServiceAccount = obj.(*corev1.ServiceAccount)

	// Don't proceed if this doesn't have annotation "sa-iamrole/managed = true"
	if val, ok := sa.ObjectMeta.Annotations[managedAnnotationKey]; ok && val == "true" {
		return
	}

	// We only treat ServiceAccounts that have an annotation of the form:
	//     eks.amazonaws.com/role-arn: arn:aws:iam::<ACCOUNT_ID>:role/<IAM_ROLE_NAME>
	//
	// We also have a strict naming convention for the IAM_ROLE_NAME. If the IAM_ROLE_NAME in this
	// ServiceAccount's annotation doesn't match
	//     (prefix_)namespace_name
	// then we ignore the event.
	if val, ok := sa.ObjectMeta.Annotations[roleAnnotationKey]; ok {
		if val == c.iam.MakeRoleARN(
			sa.ObjectMeta.Name,
			sa.ObjectMeta.Namespace,
		) {
			var key string
			var err error

			if key, err = cache.MetaNamespaceKeyFunc(obj); err != nil {
				utilruntime.HandleError(err)
				return
			}
			c.workqueue.Add(key)
		}
	}
}

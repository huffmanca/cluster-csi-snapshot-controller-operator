package operator

import (
	"fmt"
	"os"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextinformersv1beta1 "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions/apiextensions/v1beta1"
	apiextlistersv1beta1 "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1beta1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	appsclientv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	appslisterv1 "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("csi_snapshot_controller_operator")
var deploymentVersionHashKey = operatorv1.GroupName + "/rvs-hash"

const (
	targetName                = "csi-snapshot-controller"
	targetNamespace           = "openshift-csi-snapshot-controller"
	targetNameSpaceController = "openshift-csi-controller"
	targetNameOperator        = "openshift-csi-snapshot-controller-operator"
	targetNameController      = "openshift-csi-snapshot-controller"
	globalConfigName          = "cluster"

	operatorSelfName       = "operator"
	operatorVersionEnvName = "OPERATOR_IMAGE_VERSION"
	operandVersionEnvName  = "OPERAND_IMAGE_VERSION"
	operandImageEnvName    = "IMAGE"

	machineConfigNamespace = "openshift-config-managed"
	userConfigNamespace    = "openshift-config"

	maxRetries = 15
)

// static environment variables from operator deployment
var (
	csiSnapshotControllerImage = os.Getenv(targetNameController)

	operatorVersion = os.Getenv(operatorVersionEnvName)

	crdNames = []string{"volumesnapshotclasses.snapshot.storage.k8s.io", "volumesnapshotcontents.snapshot.storage.k8s.io", "volumesnapshots.snapshot.storage.k8s.io"}
)

type csiSnapshotOperator struct {
	vStore *versionStore

	client        OperatorClient
	kubeClient    kubernetes.Interface
	versionGetter status.VersionGetter
	eventRecorder events.Recorder

	syncHandler func(ic string) error

	crdLister       apiextlistersv1beta1.CustomResourceDefinitionLister
	crdListerSyncer cache.InformerSynced
	crdClient       apiextclient.Interface

	deployLister       appslisterv1.DeploymentLister
	deployListerSynced cache.InformerSynced
	deployClient       appsclientv1.AppsV1Interface

	queue workqueue.RateLimitingInterface

	stopCh <-chan struct{}
}

func NewCSISnapshotControllerOperator(
	client OperatorClient,
	crdInformer apiextinformersv1beta1.CustomResourceDefinitionInformer,
	crdClient apiextclient.Interface,
	deployInformer appsinformersv1.DeploymentInformer,
	deployClient appsclientv1.AppsV1Interface,
	kubeClient kubernetes.Interface,
	versionGetter status.VersionGetter,
	eventRecorder events.Recorder,
) *csiSnapshotOperator {
	csiOperator := &csiSnapshotOperator{
		client:        client,
		crdClient:     crdClient,
		deployClient:  deployClient,
		kubeClient:    kubeClient,
		versionGetter: versionGetter,
		eventRecorder: eventRecorder,
		vStore:        newVersionStore(),
		queue:         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "csi-snapshot-controller"),
	}

	for _, i := range []cache.SharedIndexInformer{
		crdInformer.Informer(),
	} {
		i.AddEventHandler(csiOperator.eventHandler())
	}

	csiOperator.syncHandler = csiOperator.sync

	csiOperator.crdLister = crdInformer.Lister()
	csiOperator.crdListerSyncer = crdInformer.Informer().HasSynced

	csiOperator.deployLister = deployInformer.Lister()
	csiOperator.deployListerSynced = deployInformer.Informer().HasSynced

	csiOperator.vStore.Set("operator", os.Getenv("RELEASE_VERSION"))

	return csiOperator
}

func (c *csiSnapshotOperator) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer c.queue.ShutDown()

	c.stopCh = stopCh

	for i := 0; i < workers; i++ {
		go wait.Until(c.worker, time.Second, stopCh)
	}
	<-stopCh
}

func (c *csiSnapshotOperator) sync(key string) error {
	instance, err := c.client.GetOperatorInstance()
	if err != nil {
		return err
	}

	if instance.Spec.ManagementState != operatorv1.Managed {
		return nil // TODO do something better for all states
	}

	instanceCopy := instance.DeepCopy()

	// Watch CRs for:
	// - CSISnapshots
	// - Deployments
	// - CRD
	// - Status?

	// Ensure the CSISnapshotController deployment exists and matches the default
	// If it doesn't exist, create it.
	// If it does exist and doesn't match, overwrite it
	startTime := time.Now()
	klog.Info("Starting syncing operator at ", startTime)
	defer func() {
		klog.Info("Finished syncing operator at ", time.Since(startTime))
	}()

	syncErr := c.handleSync(instanceCopy)
	c.updateSyncError(&instanceCopy.Status.OperatorStatus, syncErr)

	if _, _, err := v1helpers.UpdateStatus(c.client, func(status *operatorv1.OperatorStatus) error {
		// store a copy of our starting conditions, we need to preserve last transition time
		originalConditions := status.DeepCopy().Conditions

		// copy over everything else
		instanceCopy.Status.OperatorStatus.DeepCopyInto(status)

		// restore the starting conditions
		status.Conditions = originalConditions

		// manually update the conditions while preserving last transition time
		for _, condition := range instanceCopy.Status.Conditions {
			v1helpers.SetOperatorCondition(&status.Conditions, condition)
		}
		return nil
	}); err != nil {
		klog.Errorf("failed to update status: %v", err)
		if syncErr == nil {
			syncErr = err
		}
	}

	return syncErr
}

func (c *csiSnapshotOperator) updateSyncError(status *operatorv1.OperatorStatus, err error) {
	if err != nil {
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:    operatorv1.OperatorStatusTypeDegraded,
				Status:  operatorv1.ConditionTrue,
				Reason:  "OperatorSync",
				Message: err.Error(),
			})
	} else {
		v1helpers.SetOperatorCondition(&status.Conditions,
			operatorv1.OperatorCondition{
				Type:   operatorv1.OperatorStatusTypeDegraded,
				Status: operatorv1.ConditionFalse,
			})
	}
}

func (c *csiSnapshotOperator) handleSync(instance *operatorv1.CSISnapshotController) error {
	if err := c.syncCustomResourceDefinitions(); err != nil {
		return fmt.Errorf("failed to sync CRDs: %s", err)
	}
	if err := c.syncDeployments(); err != nil {
		return fmt.Errorf("failed to sync Deployments: %s", err)
	}
	if err := c.syncStatus(instance); err != nil {
		return fmt.Errorf("failed to sync status: %s", err)
	}
	return nil
}

func (c *csiSnapshotOperator) syncStatus(instance *operatorv1.CSISnapshotController) error {
	// The operator does not have any prerequisites (at least now)
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypePrereqsSatisfied,
			Status: operatorv1.ConditionTrue,
		})
	// The operator is always upgradeable (at least now)
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeUpgradeable,
			Status: operatorv1.ConditionTrue,
		})

	// TODO: check that deployment has enough replicas
	// TODO: check that deployment is fully updated to newest version
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeAvailable,
			Status: operatorv1.ConditionTrue,
		})
	v1helpers.SetOperatorCondition(&instance.Status.OperatorStatus.Conditions,
		operatorv1.OperatorCondition{
			Type:   operatorv1.OperatorStatusTypeProgressing,
			Status: operatorv1.ConditionFalse,
		})
	return nil
}

func (c *csiSnapshotOperator) enqueue(obj interface{}) {
	// we're filtering out config maps that are "leader" based and we don't have logic around them
	// resyncing on these causes the operator to sync every 14s for no good reason
	if cm, ok := obj.(*corev1.ConfigMap); ok && cm.GetAnnotations() != nil && cm.GetAnnotations()[resourcelock.LeaderElectionRecordAnnotationKey] != "" {
		return
	}
	workQueueKey := fmt.Sprintf("%s/%s", targetNamespace, targetName)
	c.queue.Add(workQueueKey)
}

func (c *csiSnapshotOperator) eventHandler() cache.ResourceEventHandler {
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.enqueue(obj)
		},
		UpdateFunc: func(old, new interface{}) {
			c.enqueue(new)
		},
		DeleteFunc: func(obj interface{}) {
			c.enqueue(obj)
		},
	}
}

func (c *csiSnapshotOperator) worker() {
	for c.processNextWorkItem() {
	}
}

func (c *csiSnapshotOperator) processNextWorkItem() bool {
	key, quit := c.queue.Get()
	if quit {
		return false
	}
	defer c.queue.Done(key)

	err := c.syncHandler(key.(string))
	c.handleErr(err, key)

	return true
}

func (c *csiSnapshotOperator) handleErr(err error, key interface{}) {
	if err == nil {
		c.queue.Forget(key)
		return
	}

	if c.queue.NumRequeues(key) < maxRetries {
		klog.V(2).Infof("Error syncing operator %v: %v", key, err)
		c.queue.AddRateLimited(key)
		return
	}

	utilruntime.HandleError(err)
	klog.V(2).Infof("Dropping operator %q out of the queue: %v", key, err)
	c.queue.Forget(key)
	c.queue.AddAfter(key, 1*time.Minute)
}

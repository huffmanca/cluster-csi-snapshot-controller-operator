package operator

import (
	"errors"
	"os"

	"monis.app/go/openshift/operator"

	"github.com/openshift/cluster-csi-snapshot-controller-operator/pkg/generated"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	restclient "k8s.io/client-go/rest"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"

	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var log = logf.Log.WithName("csi_snapshot_controller_operator")
var deploymentVersionHashKey = operatorv1.GroupName + "/rvs-hash"
var crds = [...]string{"assets/volumesnapshots.yaml", "assets/volumesnapshotcontents.yaml", "assets/volumesnapshotclasses.yaml"}

const (
	clusterOperatorName       = "csisnapshot"
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
)

// static environment variables from operator deployment
var (
	csiSnapshotControllerImage = os.Getenv(targetNameController)

	operatorVersion = os.Getenv(operatorVersionEnvName)
)

type csiSnapshotOperator struct {
	client        OperatorClient
	crdClient	  *apiextclient.Clientset
	kubeConfig	  *restclient.Config
	versionGetter status.VersionGetter
	recorder      events.Recorder
}

func NewCSISnapshotControllerOperator(
	client OperatorClient,
	kubeConfig *restclient.Config,
	versionGetter status.VersionGetter,
	recorder events.Recorder,
) operator.Runner {
	csiOperator := &csiSnapshotOperator{
		client:        client,
		kubeConfig:    kubeConfig,
		crdClient:     apiextclient.NewForConfigOrDie(kubeConfig),
		versionGetter: versionGetter,
		recorder:      recorder,
	}

	return operator.New("CSISnapshotControllerOperator", csiOperator)
}

func (c *csiSnapshotOperator) initCRDs() {
	// Initialize the Snapshot Controller CRDs
	createCRD := crdClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create
	for _, file := range crds {
		crd := resourceread.ReadCustomResourceDefinitionV1Beta1OrDie(generated.MustAsset(file))
		_, updated, err := resourceapply.ApplyCustomResourceDefinition(c.crdClient, crd)
		if err != nil {
			return err
		}
	}
}

func (c *csiSnapshotOperator) Key() (metav1.Object, error) {
	return c.client.Client.OpenShiftControllerManagers().Get(globalConfigName, metav1.GetOptions{})
}

func (c *csiSnapshotOperator) Sync(obj metav1.Object) error {
	// Watch CRs for:
	// - CSISnapshots
	// - Deployments
	// - CRD
	// - Status?

	// Ensure the CSISnapshotController deployment exists and matches the default
	// If it doesn't exist, create it.
	// If it does exist and doesn't match, overwrite it

	// Update CSISnapshotController.Status

	return errors.New("unsupported function")
}
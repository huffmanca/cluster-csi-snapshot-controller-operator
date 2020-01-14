package common

import (
	"os"

	"github.com/golang/glog"

	apiext "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Builder can create a variety of kubernetes client interface
// with its embedded rest.Config.
type Builder struct {
	config *rest.Config
}

// APIExtClientOrDie returns the kubernetes client interface for extended kubernetes objects.
func (cb *Builder) APIExtClientOrDie(name string) apiext.Interface {
	return apiext.NewForConfigOrDie(rest.AddUserAgent(cb.config, name))
}

// NewBuilder returns a *ClientBuilder with the given kubeconfig.
func NewBuilder(kubeconfig string) (*Builder, error) {
	var config *rest.Config
	var err error

	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}

	if kubeconfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeconfig)
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		glog.V(4).Infof("Using in-cluster kube client config")
		config, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}

	return &Builder{
		config: config,
	}, nil
}

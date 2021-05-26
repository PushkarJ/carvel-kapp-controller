// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apiserver/client/clientset/versioned/typed/datapackaging/v1alpha1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeDataV1alpha1 struct {
	*testing.Fake
}

func (c *FakeDataV1alpha1) PackageVersions(namespace string) v1alpha1.PackageVersionInterface {
	return &FakePackageVersions{c, namespace}
}

func (c *FakeDataV1alpha1) Packages(namespace string) v1alpha1.PackageInterface {
	return &FakePackages{c, namespace}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeDataV1alpha1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}

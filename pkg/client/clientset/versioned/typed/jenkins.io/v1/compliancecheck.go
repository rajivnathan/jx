// Code generated by client-gen. DO NOT EDIT.

package v1

import (
	v1 "github.com/jenkins-x/jx/pkg/apis/jenkins.io/v1"
	scheme "github.com/jenkins-x/jx/pkg/client/clientset/versioned/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// ComplianceChecksGetter has a method to return a ComplianceCheckInterface.
// A group's client should implement this interface.
type ComplianceChecksGetter interface {
	ComplianceChecks(namespace string) ComplianceCheckInterface
}

// ComplianceCheckInterface has methods to work with ComplianceCheck resources.
type ComplianceCheckInterface interface {
	Create(*v1.ComplianceCheck) (*v1.ComplianceCheck, error)
	Update(*v1.ComplianceCheck) (*v1.ComplianceCheck, error)
	Delete(name string, options *metav1.DeleteOptions) error
	DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error
	Get(name string, options metav1.GetOptions) (*v1.ComplianceCheck, error)
	List(opts metav1.ListOptions) (*v1.ComplianceCheckList, error)
	Watch(opts metav1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.ComplianceCheck, err error)
	ComplianceCheckExpansion
}

// complianceChecks implements ComplianceCheckInterface
type complianceChecks struct {
	client rest.Interface
	ns     string
}

// newComplianceChecks returns a ComplianceChecks
func newComplianceChecks(c *JenkinsV1Client, namespace string) *complianceChecks {
	return &complianceChecks{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the complianceCheck, and returns the corresponding complianceCheck object, and an error if there is any.
func (c *complianceChecks) Get(name string, options metav1.GetOptions) (result *v1.ComplianceCheck, err error) {
	result = &v1.ComplianceCheck{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("compliancechecks").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of ComplianceChecks that match those selectors.
func (c *complianceChecks) List(opts metav1.ListOptions) (result *v1.ComplianceCheckList, err error) {
	result = &v1.ComplianceCheckList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("compliancechecks").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested complianceChecks.
func (c *complianceChecks) Watch(opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("compliancechecks").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a complianceCheck and creates it.  Returns the server's representation of the complianceCheck, and an error, if there is any.
func (c *complianceChecks) Create(complianceCheck *v1.ComplianceCheck) (result *v1.ComplianceCheck, err error) {
	result = &v1.ComplianceCheck{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("compliancechecks").
		Body(complianceCheck).
		Do().
		Into(result)
	return
}

// Update takes the representation of a complianceCheck and updates it. Returns the server's representation of the complianceCheck, and an error, if there is any.
func (c *complianceChecks) Update(complianceCheck *v1.ComplianceCheck) (result *v1.ComplianceCheck, err error) {
	result = &v1.ComplianceCheck{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("compliancechecks").
		Name(complianceCheck.Name).
		Body(complianceCheck).
		Do().
		Into(result)
	return
}

// Delete takes name of the complianceCheck and deletes it. Returns an error if one occurs.
func (c *complianceChecks) Delete(name string, options *metav1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("compliancechecks").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *complianceChecks) DeleteCollection(options *metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("compliancechecks").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched complianceCheck.
func (c *complianceChecks) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *v1.ComplianceCheck, err error) {
	result = &v1.ComplianceCheck{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("compliancechecks").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}

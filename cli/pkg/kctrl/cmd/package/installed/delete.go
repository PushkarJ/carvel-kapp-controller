// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package installed

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/spf13/cobra"
	cmdcore "github.com/vmware-tanzu/carvel-kapp-controller/cli/pkg/kctrl/cmd/core"
	"github.com/vmware-tanzu/carvel-kapp-controller/cli/pkg/kctrl/logger"
	kcv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/kappctrl/v1alpha1"
	kcpkgv1alpha1 "github.com/vmware-tanzu/carvel-kapp-controller/pkg/apis/packaging/v1alpha1"
	kcclient "github.com/vmware-tanzu/carvel-kapp-controller/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
)

type DeleteOptions struct {
	ui          ui.UI
	depsFactory cmdcore.DepsFactory
	logger      logger.Logger

	NamespaceFlags cmdcore.NamespaceFlags
	Name           string

	WaitFlags cmdcore.WaitFlags

	pkgCmdTreeOpts cmdcore.PackageCommandTreeOpts
}

func NewDeleteOptions(ui ui.UI, depsFactory cmdcore.DepsFactory, logger logger.Logger, pkgCmdTreeOpts cmdcore.PackageCommandTreeOpts) *DeleteOptions {
	return &DeleteOptions{ui: ui, depsFactory: depsFactory, logger: logger, pkgCmdTreeOpts: pkgCmdTreeOpts}
}

func NewDeleteCmd(o *DeleteOptions, flagsFactory cmdcore.FlagsFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Uninstall installed package",
		RunE:  func(_ *cobra.Command, args []string) error { return o.Run(args) },
		Example: cmdcore.Examples{
			cmdcore.Example{"Delete package install",
				[]string{"package", "installed", "delete", "-i", "cert-man"}},
		}.Description("-i", o.pkgCmdTreeOpts),
		SilenceUsage: true,
	}
	o.NamespaceFlags.Set(cmd, flagsFactory)

	if !o.pkgCmdTreeOpts.PositionalArgs {
		cmd.Flags().StringVarP(&o.Name, "package-install", "i", "", "Set installed package name (required)")
	} else {
		cmd.Use = "delete INSTALLED_PACKAGE_NAME"
	}

	o.WaitFlags.Set(cmd, flagsFactory, &cmdcore.WaitFlagsOpts{
		AllowDisableWait: false,
		DefaultInterval:  1 * time.Second,
		DefaultTimeout:   5 * time.Minute,
	})

	return cmd
}

func (o *DeleteOptions) Run(args []string) error {
	if o.pkgCmdTreeOpts.PositionalArgs {
		o.Name = args[0]
	}

	if len(o.Name) == 0 {
		return fmt.Errorf("Expected package install name to be non empty")
	}

	o.ui.PrintLinef("Delete package install '%s' from namespace '%s'", o.Name, o.NamespaceFlags.Name)

	err := o.ui.AskForConfirmation()
	if err != nil {
		return err
	}

	kcClient, err := o.depsFactory.KappCtrlClient()
	if err != nil {
		return nil
	}

	//TODO: Read warnings flag. Is it needed?
	dynamicClient, err := o.depsFactory.DynamicClient(cmdcore.DynamicClientOpts{})
	if err != nil {
		return nil
	}

	pkgi, err := kcClient.PackagingV1alpha1().PackageInstalls(o.NamespaceFlags.Name).Get(
		context.Background(), o.Name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return err
		}

		o.ui.PrintLinef("Could not find package install '%s' in namespace '%s'. Cleaning up created resources.", o.Name, o.NamespaceFlags.Name)

		return o.cleanUpIfInstallNotFound(dynamicClient)
	}

	o.ui.PrintLinef("Deleting package install '%s' from namespace '%s'", o.Name, o.NamespaceFlags.Name)

	err = kcClient.PackagingV1alpha1().PackageInstalls(o.NamespaceFlags.Name).Delete(
		context.Background(), o.Name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	o.ui.PrintLinef("Waiting for deletion of package install '%s' from namespace '%s'", o.Name, o.NamespaceFlags.Name)

	err = o.waitForResourceDelete(kcClient)
	if err != nil {
		return err
	}

	return o.deleteInstallCreatedResources(pkgi, dynamicClient)
}

// deletePkgPluginCreatedResources deletes the associated resources which were installed upon installation of the PackageInstall CR
func (o *DeleteOptions) deleteInstallCreatedResources(pkgInstall *kcpkgv1alpha1.PackageInstall, dynamicClient dynamic.Interface) error {
	deletedResources := map[CreatedResourceKind]bool{}
	for k, resourceName := range pkgInstall.GetAnnotations() {
		split := strings.Split(k, "/")
		if len(split) <= 1 {
			continue
		}

		resourceKind := CreatedResourceKind(strings.TrimPrefix(split[1], KctrlPkgAnnotationPrefix))

		// To support older versions of Tanzu CLI. To be deprecated
		resourceKind = CreatedResourceKind(strings.TrimPrefix(resourceKind.AsString(), TanzuPkgAnnotationPrefix))
		if deletedResources[resourceKind] {
			continue
		}

		if CreatedResourceKind(resourceKind).Resource() == "" {
			continue
		}

		deletedResources[resourceKind] = true

		var apiGroup, version, namespace string
		if resourceKind == KindClusterRole || resourceKind == KindClusterRoleBinding {
			apiGroup = rbacv1.SchemeGroupVersion.Group
			version = rbacv1.SchemeGroupVersion.Version
		} else {
			apiGroup = corev1.SchemeGroupVersion.Group
			version = corev1.SchemeGroupVersion.Version
			namespace = o.NamespaceFlags.Name
		}

		o.ui.PrintLinef("Deleting '%s': %s", resourceKind, resourceName)

		err := o.deleteResourceUsingGVR(schema.GroupVersionResource{
			Group:    apiGroup,
			Version:  version,
			Resource: resourceKind.Resource(),
		}, resourceName, namespace, dynamicClient)
		if err != nil {
			return err
		}
	}

	return nil
}

func (o *DeleteOptions) cleanUpIfInstallNotFound(dynamicClient dynamic.Interface) error {
	err := o.deleteIfExistsAndOwned(
		schema.GroupVersionResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Version:  corev1.SchemeGroupVersion.Version,
			Resource: KindServiceAccount.Resource(),
		}, KindServiceAccount.Name(o.Name, o.NamespaceFlags.Name), o.NamespaceFlags.Name, dynamicClient)
	if err != nil {
		return err
	}

	err = o.deleteIfExistsAndOwned(
		schema.GroupVersionResource{
			Group:    corev1.SchemeGroupVersion.Group,
			Version:  corev1.SchemeGroupVersion.Version,
			Resource: KindServiceAccount.Resource(),
		}, KindClusterRole.Name(o.Name, o.NamespaceFlags.Name), o.NamespaceFlags.Name, dynamicClient)
	if err != nil {
		return err
	}

	err = o.deleteIfExistsAndOwned(
		schema.GroupVersionResource{
			Group:    rbacv1.SchemeGroupVersion.Group,
			Version:  rbacv1.SchemeGroupVersion.Version,
			Resource: KindClusterRole.Resource(),
		}, KindClusterRole.Name(o.Name, o.NamespaceFlags.Name), "", dynamicClient)
	if err != nil {
		return err
	}

	err = o.deleteIfExistsAndOwned(
		schema.GroupVersionResource{
			Group:    rbacv1.SchemeGroupVersion.Group,
			Version:  rbacv1.SchemeGroupVersion.Version,
			Resource: KindClusterRoleBinding.Resource(),
		}, KindClusterRoleBinding.Name(o.Name, o.NamespaceFlags.Name), "", dynamicClient)
	if err != nil {
		return err
	}

	return nil
}

func (o *DeleteOptions) deleteIfExistsAndOwned(groupVersionResource schema.GroupVersionResource, name string, namespace string, dynamicClient dynamic.Interface) error {
	resource, err := dynamicClient.Resource(groupVersionResource).Namespace(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			// Ignoring NotFound errors
			return err
		}
		return nil
	}

	annotations := resource.GetAnnotations()
	pkgiIdentifier := fmt.Sprintf("%s-%s", o.Name, o.NamespaceFlags.Name)

	val, found := annotations[KctrlPkgAnnotation]
	if !found || val != pkgiIdentifier {
		// Do not delete if the resource is not owned by the package, but no need to error out

		// To support older version of Tanzu CLI. To be deprecated
		val, found = annotations[TanzuPkgAnnotation]
		if !found || val != pkgiIdentifier {
			return nil
		}
	}

	o.ui.PrintLinef("Deleting '%s': %s", groupVersionResource.Resource, name)
	err = dynamicClient.Resource(groupVersionResource).Namespace(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (o *DeleteOptions) deleteResourceUsingGVR(groupVersionResource schema.GroupVersionResource, name string, namespace string, dynamicClient dynamic.Interface) error {
	err := dynamicClient.Resource(groupVersionResource).Namespace(namespace).Delete(context.TODO(), name, metav1.DeleteOptions{})
	if err != nil {
		return err
	}

	return nil
}

func (o *DeleteOptions) waitForResourceDelete(kcClient kcclient.Interface) error {
	msgsUI := cmdcore.NewDedupingMessagesUI(cmdcore.NewPlainMessagesUI(o.ui))
	description := getPackageInstallDescription(o.Name, o.NamespaceFlags.Name)

	if err := wait.Poll(o.WaitFlags.CheckInterval, o.WaitFlags.Timeout, func() (bool, error) {
		resource, err := kcClient.PackagingV1alpha1().PackageInstalls(o.NamespaceFlags.Name).Get(
			context.Background(), o.Name, metav1.GetOptions{},
		)
		if err != nil {
			if errors.IsNotFound(err) {
				msgsUI.NotifySection("%s: DeletionSucceeded", description)
				return true, nil
			}
			return false, err
		}
		if resource.Generation != resource.Status.ObservedGeneration {
			// Should wait for generation to be observed before checking the reconciliation status so that we know we are checking the new spec
			return false, nil
		}
		status := resource.Status.GenericStatus

		for _, cond := range status.Conditions {
			msgsUI.NotifySection("%s: %s", description, cond.Type)

			if cond.Type == kcv1alpha1.DeleteFailed && cond.Status == corev1.ConditionTrue {
				return false, fmt.Errorf("%s: Deleting: %s. %s", description, status.UsefulErrorMessage, status.FriendlyDescription)
			}
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("%s: Deleting: %s", description, err)
	}

	return nil
}

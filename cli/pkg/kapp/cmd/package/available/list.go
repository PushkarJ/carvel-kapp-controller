// Copyright 2020 VMware, Inc.
// SPDX-License-Identifier: Apache-2.0

package available

import (
	"context"
	"fmt"

	"github.com/cppforlife/go-cli-ui/ui"
	uitable "github.com/cppforlife/go-cli-ui/ui/table"
	"github.com/spf13/cobra"
	cmdcore "github.com/vmware-tanzu/carvel-kapp-controller/cli/pkg/kapp/cmd/core"
	"github.com/vmware-tanzu/carvel-kapp-controller/cli/pkg/kapp/logger"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
)

type ListOptions struct {
	ui          ui.UI
	depsFactory cmdcore.DepsFactory
	logger      logger.Logger

	NamespaceFlags cmdcore.NamespaceFlags
	AllNamespaces  bool

	Name string
}

func NewListOptions(ui ui.UI, depsFactory cmdcore.DepsFactory, logger logger.Logger) *ListOptions {
	return &ListOptions{ui: ui, depsFactory: depsFactory, logger: logger}
}

func NewListCmd(o *ListOptions, flagsFactory cmdcore.FlagsFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"l", "ls"},
		Short:   "List available packages in a namespace",
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}
	o.NamespaceFlags.Set(cmd, flagsFactory)
	cmd.Flags().BoolVarP(&o.AllNamespaces, "all-namespaces", "A", false, "List available packages")

	cmd.Flags().StringVarP(&o.Name, "package", "p", "", "List all available versions of package")

	return cmd
}

func (o *ListOptions) Run() error {
	if o.Name != "" {
		return o.listPackages()
	}
	return o.listPackageMetadatas()
}

func (o *ListOptions) listPackageMetadatas() error {
	tableTitle := fmt.Sprintf("Available packages in namespace '%s'", o.NamespaceFlags.Name)
	nsHeader := uitable.NewHeader("Namespace")
	nsHeader.Hidden = true

	if o.AllNamespaces {
		o.NamespaceFlags.Name = ""
		tableTitle = "Available packages in all namespaces"
		nsHeader.Hidden = false
	}

	client, err := o.depsFactory.PackageClient()
	if err != nil {
		return err
	}

	pkgmList, err := client.DataV1alpha1().PackageMetadatas(
		o.NamespaceFlags.Name).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return err
	}

	table := uitable.Table{
		Title: tableTitle,
		// TODO these arent really packages
		Content: "Packages Available",

		Header: []uitable.Header{
			nsHeader,
			uitable.NewHeader("Name"),
			uitable.NewHeader("Display-Name"),
			uitable.NewHeader("Short-Description"),
		},

		SortBy: []uitable.ColumnSort{
			{Column: 0, Asc: true},
			{Column: 1, Asc: true},
		},
	}

	for _, pkgm := range pkgmList.Items {
		table.Rows = append(table.Rows, []uitable.Value{
			cmdcore.NewValueNamespace(pkgm.Namespace),
			uitable.NewValueString(pkgm.Name),
			uitable.NewValueString(pkgm.Spec.DisplayName),
			uitable.NewValueString(pkgm.Spec.ShortDescription),
		})
	}

	o.ui.PrintTable(table)

	return err
}

func (o *ListOptions) listPackages() error {
	// TODO figure out better naming... these are packages
	tableTitle := fmt.Sprintf("Available package versions for '%s' in namespace '%s'", o.Name, o.NamespaceFlags.Name)
	nsHeader := uitable.NewHeader("Namespace")
	nsHeader.Hidden = true

	if o.AllNamespaces {
		o.NamespaceFlags.Name = ""
		tableTitle = fmt.Sprintf("Available package versions for '%s' in all namespaces", o.Name)
		nsHeader.Hidden = false
	}

	client, err := o.depsFactory.PackageClient()
	if err != nil {
		return err
	}

	listOpts := metav1.ListOptions{FieldSelector: fields.Set{"spec.refName": o.Name}.String()}
	pkgList, err := client.DataV1alpha1().Packages(
		o.NamespaceFlags.Name).List(context.Background(), listOpts)
	if err != nil {
		return err
	}

	table := uitable.Table{
		Title:   tableTitle,
		Content: "Package Versions Available",

		Header: []uitable.Header{
			nsHeader,
			uitable.NewHeader("Name"),
			uitable.NewHeader("Version"),
			uitable.NewHeader("Released-At"),
		},

		SortBy: []uitable.ColumnSort{
			{Column: 0, Asc: true},
			{Column: 1, Asc: true},
		},
	}

	for _, pkg := range pkgList.Items {
		table.Rows = append(table.Rows, []uitable.Value{
			cmdcore.NewValueNamespace(pkg.Namespace),
			uitable.NewValueString(pkg.Spec.RefName),
			uitable.NewValueString(pkg.Spec.Version),
			uitable.NewValueString(pkg.Spec.ReleasedAt.String()),
		})
	}

	o.ui.PrintTable(table)

	return err
}
/*
Copyright 2021 VMware, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package commands

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cartov1alpha1 "github.com/vmware-tanzu/apps-cli-plugin/pkg/apis/cartographer/v1alpha1"
	cli "github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/tree"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/validation"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/completion"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/flags"
)

const (
	allNamespacesFlag = "all-namespaces"
)

type WorkloadTreeOptions struct {
	Namespace     string
	Name          string
	AllNamespaces bool
	Component     string
	Since         time.Duration
	Timestamps    bool
}

var (
	_ validation.Validatable = (*WorkloadTreeOptions)(nil)
	_ cli.Executable         = (*WorkloadTreeOptions)(nil)
)

func (opts *WorkloadTreeOptions) Exec(ctx context.Context, c *cli.Config) error {
	workload := &cartov1alpha1.Workload{}
	err := c.Get(ctx, client.ObjectKey{Namespace: opts.Namespace, Name: opts.Name}, workload)
	if err != nil {
		if !apierrs.IsNotFound(err) {
			return err
		}
		c.Errorf("Workload %q not found\n", fmt.Sprintf("%s/%s", opts.Namespace, opts.Name))
		return cli.SilenceError(err)
	}

	restConfig := c.KubeRestConfig()
	restConfig.QPS = 1000   //need discussions and alignement from Rashed
	restConfig.Burst = 1000 //need discussions and alignement from Rashed
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to construct dynamic client: %w", err)
	}
	apis, err := tree.FindAPIs(ctx, c)
	if err != nil {
		return err
	}

	kind := "workloads"
	name := opts.Name

	var api tree.ApiResource
	if k, ok := tree.OverrideType(kind, apis); ok {
		// c.Infof("kind=%s override found: %s", kind, k.GroupVersionResource())
		api = k
	} else {
		apiResults := apis.Lookup(kind)
		// c.Infof("kind matches=%v", apiResults)
		if len(apiResults) == 0 {
			return fmt.Errorf("could not find api kind %q", kind)
		} else if len(apiResults) > 1 {
			names := make([]string, 0, len(apiResults))
			for _, a := range apiResults {
				names = append(names, tree.FullAPIName(a))
			}
			return fmt.Errorf("ambiguous kind %q. use one of these as the KIND disambiguate: [%s]", kind,
				strings.Join(names, ", "))
		}
		api = apiResults[0]
	}
	ns := opts.Namespace
	command := cli.CommandFromContext(ctx)
	allNs, err := command.Flags().GetBool(allNamespacesFlag)
	if err != nil {
		allNs = false
	}

	var ri dynamic.ResourceInterface
	if api.ApiRsrc.Namespaced {
		ri = dyn.Resource(api.GroupVersionResource()).Namespace(ns)
	} else {
		ri = dyn.Resource(api.GroupVersionResource())
	}
	obj, err := ri.Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get %s/%s: %w", kind, name, err)
	}
	apiObjects, err := tree.GetAllResources(dyn, apis.Resources(), allNs)
	if err != nil {
		return fmt.Errorf("error while querying api objects: %w", err)
	}

	objs := tree.NewObjectDirectory(apiObjects)
	if len(objs.Ownership[obj.GetUID()]) == 0 {
		fmt.Println("No resources are owned by this object through ownerReferences.")
		return nil
	}
	tree.TreeView(os.Stderr, objs, *obj)
	return nil

}
func (opts *WorkloadTreeOptions) Validate(ctx context.Context) validation.FieldErrors {
	errs := validation.FieldErrors{}

	if opts.Namespace == "" {
		errs = errs.Also(validation.ErrMissingField(flags.NamespaceFlagName))
	}

	if opts.Name == "" {
		errs = errs.Also(validation.ErrMissingField(cli.NameArgumentName))
	} else {
		errs = errs.Also(validation.K8sName(opts.Name, cli.NameArgumentName))
	}

	if opts.Since < 0 {
		errs = errs.Also(validation.ErrInvalidValue(opts.Since, flags.SinceFlagName))
	}

	errs = errs.Also(validation.K8sLabelValue(opts.Component, flags.ComponentFlagName))
	return errs
}
func NewWorkloadTreeCommand(ctx context.Context, c *cli.Config) *cobra.Command {
	opts := &WorkloadTreeOptions{}

	cmd := &cobra.Command{
		Use:   "tree",
		Short: "Show sub-resources of the workload object",
		Long: strings.TrimSpace(`
Tree for the sub-resource with stauses and details for workload
`),
		Example: strings.Join([]string{
			fmt.Sprintf("%s workload tree my-workload", c.Name),
			fmt.Sprintf("%s workload tree my-workload %s 1h", c.Name, flags.SinceFlagName),
		}, "\n"),
		PreRunE:           cli.ValidateE(ctx, opts),
		RunE:              cli.ExecE(ctx, c, opts),
		ValidArgsFunction: completion.SuggestWorkloadNames(ctx, c),
	}

	cli.Args(cmd,
		cli.NameArg(&opts.Name),
	)

	cli.NamespaceFlag(ctx, cmd, c, &opts.Namespace)
	cmd.Flags().BoolP(allNamespacesFlag, "A", false, "query all objects in all API groups, both namespaced and non-namespaced")
	cmd.Flags().StringVar(&opts.Component, cli.StripDash(flags.ComponentFlagName), "", "workload component `name` (e.g. build)")
	cmd.RegisterFlagCompletionFunc(cli.StripDash(flags.ComponentFlagName), completion.SuggestComponentNames(ctx, c))
	cmd.Flags().BoolVarP(&opts.Timestamps, cli.StripDash(flags.TimestampFlagName), "t", false, "print timestamp for each log line")
	cmd.Flags().DurationVar(&opts.Since, cli.StripDash(flags.SinceFlagName), time.Second, "time `duration` to start reading logs from")

	cmd.RegisterFlagCompletionFunc(cli.StripDash(flags.SinceFlagName), completion.SuggestDurationUnits(ctx, completion.CommonDurationUnits))
	return cmd
}

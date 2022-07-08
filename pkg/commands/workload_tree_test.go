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

package commands_test

import (
	"testing"
	"time"

	diemetav1 "dies.dev/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// "github.com/vmware-tanzu/apps-cli-plugin/pkg/apis"

	cartov1alpha1 "github.com/vmware-tanzu/apps-cli-plugin/pkg/apis/cartographer/v1alpha1"
	cli "github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime"
	clitesting "github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/testing"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/validation"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/commands"
	diecartov1alpha1 "github.com/vmware-tanzu/apps-cli-plugin/pkg/dies/cartographer/v1alpha1"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/flags"
)

func TestWorkloadTreeOptionsValidate(t *testing.T) {
	table := clitesting.ValidatableTestSuite{
		{
			Name:        "invalid empty",
			Validatable: &commands.WorkloadTreeOptions{},
			ExpectFieldErrors: validation.FieldErrors{}.Also(
				validation.ErrMissingField(flags.NamespaceFlagName),
				validation.ErrMissingField(cli.NameArgumentName),
			),
		},
		{
			Name: "valid",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-workload",
			},
			ShouldValidate: true,
		},
		{
			Name: "invalid name",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-",
			},
			ExpectFieldErrors: validation.ErrInvalidValue("my-", cli.NameArgumentName),
		},
		{
			Name: "since",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-workload",
				Since:     time.Minute,
			},
			ShouldValidate: true,
		},
		{
			Name: "invalid since",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-workload",
				Since:     -1,
			},
			ExpectFieldErrors: validation.ErrInvalidValue(-1*time.Nanosecond, flags.SinceFlagName),
		},
		{
			Name: "component",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-workload",
				Component: "build",
			},
			ShouldValidate: true,
		},
		{
			Name: "invalid component",
			Validatable: &commands.WorkloadTreeOptions{
				Namespace: "default",
				Name:      "my-workload",
				Component: "---",
			},
			ExpectFieldErrors: validation.ErrInvalidValue("---", flags.ComponentFlagName),
		},
	}
	table.Run(t)
}
func TestWorkloadTreeCommand(t *testing.T) {
	workloadName := "test-workload"
	defaultNamespace := "default"

	scheme := runtime.NewScheme()
	_ = cartov1alpha1.AddToScheme(scheme)

	parent := diecartov1alpha1.WorkloadBlank.
		MetadataDie(func(d *diemetav1.ObjectMetaDie) {
			d.Name(workloadName)
			d.Namespace(defaultNamespace)
		})

	table := clitesting.CommandTestSuite{
		{
			Name:        "empty",
			Args:        []string{},
			ShouldError: true,
		},
		{
			Name:        "invalid namespace",
			Args:        []string{flags.NamespaceFlagName, "other-namespce", workloadName},
			ShouldError: true,
			GivenObjects: []client.Object{
				parent,
			},
			ExpectOutput: `
Workload "other-namespce/test-workload" not found
`,
		},
		{
			Name:        "failed to get workload",
			Args:        []string{flags.NamespaceFlagName, defaultNamespace, workloadName},
			ShouldError: true,
			WithReactors: []clitesting.ReactionFunc{
				clitesting.InduceFailure("get", "Workload"),
			},
		},

		{
			Name: "missing workload",
			Args: []string{flags.NamespaceFlagName, defaultNamespace, workloadName},
			ExpectOutput: `
Workload "default/test-workload" not found
`,
			ShouldError: true,
		},
		// {
		// 	Focus: true,
		// 	Name:  "Show sub-resources of the workload object",
		// 	Args:  []string{flags.NamespaceFlagName, defaultNamespace, workloadName},

		// 	GivenObjects: []client.Object{
		// 		parent.
		// 			MetadataDie(func(d *diemetav1.ObjectMetaDie) {
		// 				d.AddLabel(apis.AppPartOfLabelName, workloadName)
		// 			}).
		// 			StatusDie(func(d *diecartov1alpha1.WorkloadStatusDie) {
		// 				d.ConditionsDie(
		// 					diecartov1alpha1.WorkloadConditionReadyBlank.
		// 						Status(metav1.ConditionUnknown).Reason("Workload Reason").
		// 						Message("a hopefully informative message about what went wrong"),
		// 				)
		// 			}),
		// 	},
		// 	ExpectOutput: `
		// 	NAMESPACE  NAME                                          READY  REASON               AGE
		// 	`,
		// },
	}
	table.Run(t, scheme, commands.NewWorkloadTreeCommand)
}

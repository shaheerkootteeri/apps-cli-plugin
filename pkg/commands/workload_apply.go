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
	"strings"
	"time"

	"github.com/spf13/cobra"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cartov1alpha1 "github.com/vmware-tanzu/apps-cli-plugin/pkg/apis/cartographer/v1alpha1"
	cli "github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/validation"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/cli-runtime/wait"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/completion"
	"github.com/vmware-tanzu/apps-cli-plugin/pkg/flags"
)

type WorkloadApplyOptions struct {
	WorkloadOptions
	UpdateStrategy string
}

var (
	_ validation.Validatable = (*WorkloadApplyOptions)(nil)
	_ cli.Executable         = (*WorkloadApplyOptions)(nil)
	_ cli.DryRunable         = (*WorkloadApplyOptions)(nil)
)

const (
	mergeUpdateStrategy   = "merge"
	replaceUpdateStrategy = "replace"
)

type WorkloadTimeoutStashKey struct{}

func (opts *WorkloadApplyOptions) Validate(ctx context.Context) validation.FieldErrors {
	errs := validation.FieldErrors{}
	errs = errs.Also(opts.WorkloadOptions.Validate(ctx))

	if opts.UpdateStrategy != "" && cli.CommandFromContext(ctx).Flags().Changed(cli.StripDash(flags.UpdateStrategyFlagName)) {
		if opts.FilePath == "" {
			errs = errs.Also(validation.ErrMissingField(flags.FilePathFlagName))
		}
		errs = errs.Also(validation.Enum(opts.UpdateStrategy, flags.UpdateStrategyFlagName, []string{mergeUpdateStrategy, replaceUpdateStrategy}))
	}

	return errs
}

func (opts *WorkloadApplyOptions) Exec(ctx context.Context, c *cli.Config) error {
	var okToApply bool
	shouldPrint := opts.Output == "" || (opts.Output != "" && !opts.Yes)

	fileWorkload := &cartov1alpha1.Workload{}
	if opts.FilePath != "" {
		cli.PrintPromptWithEmoji(shouldPrint, c.Emoji, cli.Exclamation, fmt.Sprintf("WARNING: Configuration file update strategy is changing. By default, provided configuration files will replace rather than merge existing configuration. The change will take place in the January 2024 TAP release (use %q to control strategy explicitly).\n\n", flags.UpdateStrategyFlagName))
		if err := opts.WorkloadOptions.LoadInputWorkload(c.Stdin, fileWorkload); err != nil {
			return err
		}

		if opts.Name == "" {
			opts.Name = fileWorkload.Name
		}
		if fileWorkload.Namespace != "" && !cli.CommandFromContext(ctx).Flags().Changed(cli.StripDash(flags.NamespaceFlagName)) {
			opts.Namespace = fileWorkload.Namespace
		}
	}

	// validate that a namespace and name are provided
	errs := validation.FieldErrors{}
	if opts.Name == "" {
		errs = errs.Also(validation.ErrMissingField(cli.NameArgumentName))
	}
	if opts.Namespace == "" {
		errs = errs.Also(validation.ErrMissingField(flags.NamespaceFlagName))
	}
	if err := errs.ToAggregate(); err != nil {
		return err
	}

	workload := &cartov1alpha1.Workload{}
	var currentWorkload *cartov1alpha1.Workload
	err := c.Get(ctx, client.ObjectKey{Namespace: opts.Namespace, Name: opts.Name}, workload)
	if err == nil {
		currentWorkload = workload.DeepCopy()
	} else {
		if !apierrs.IsNotFound(err) {
			return err
		}
		if apierrs.IsNotFound(err) {
			if nsErr := validateNamespace(ctx, c, opts.Namespace); nsErr != nil {
				return nsErr
			}
		}
	}

	if opts.UpdateStrategy == mergeUpdateStrategy {
		if opts.FilePath != "" {
			var serviceAccountCopy string
			// avoid passing a nil pointer to MergeServiceAccountName func
			if fileWorkload.Spec.ServiceAccountName != nil {
				serviceAccountCopy = *fileWorkload.Spec.ServiceAccountName
			}

			workload.Spec.MergeServiceAccountName(serviceAccountCopy)
		}
		workload.Merge(fileWorkload)
	}

	if opts.UpdateStrategy == replaceUpdateStrategy {
		// assign all the file workload fields to the workload in the cluster
		workload = fileWorkload

		// if there is a workload in the cluster with all metadata populated
		// re assign the system populated fields so we won't find an error because of some missing fields
		workload.ReplaceMetadata(currentWorkload)
	}

	workload.Name = opts.Name
	workload.Namespace = opts.Namespace
	workloadExists := currentWorkload != nil

	ctx = opts.ApplyOptionsToWorkload(ctx, currentWorkload, workload)

	// validate complex flag interactions with existing state
	errs = workload.Validate()
	if err := errs.ToAggregate(); err != nil {
		// show command usage before error
		cli.CommandFromContext(ctx).SilenceUsage = false
		return err
	}

	if opts.DryRun {
		cli.DryRunResource(ctx, workload, workload.GetGroupVersionKind())
		return nil
	}

	if opts.useLSP(currentWorkload) {
		if err := checkLSPHealth(ctx, c); err != nil {
			return err
		}
	}

	if err := opts.PublishLocalSource(ctx, c, currentWorkload, workload, shouldPrint); err != nil {
		return err
	}
	opts.ManageLocalSourceProxyAnnotation(fileWorkload, currentWorkload, workload)

	// if output flag was not set or it was not used with yes flag, then proceed to show
	// surveys and all other output
	if shouldPrint {
		var okToCreate, okToUpdate bool
		var createError, updateError error
		// If there is no workload, create a new one
		if !workloadExists {
			okToCreate, createError = opts.Create(ctx, c, workload)
			if createError != nil {
				return createError
			}
		} else {
			okToUpdate, updateError = opts.Update(ctx, c, currentWorkload, workload)
			if updateError != nil {
				return updateError
			}
		}

		okToApply = okToCreate || okToUpdate
		if okToApply {
			c.Printf("\n")
			DisplayCommandNextSteps(c, workload)
			c.Printf("\n")
		}
	} else if opts.Output != "" && opts.Yes {
		// since there are no prompts, set okToApply to true (accepted through --yes)
		okToApply = opts.Yes
		if !workloadExists {
			if err := c.Create(ctx, workload); err != nil {
				return err
			}
		} else {
			if err := c.Update(ctx, workload); err != nil {
				return err
			}
		}
	}

	if okToApply {
		anyTail := opts.Tail || opts.TailTimestamps
		var workers []wait.Worker
		if opts.Wait || anyTail {
			cli.PrintPrompt(shouldPrint, c.Infof, "Waiting for workload %q to become ready...\n", opts.Name)

			if workloadExists {
				statusChangeWorkers := []wait.Worker{getStatusChangeWorker(c, currentWorkload)}

				timeout := opts.WaitTimeout
				stashedTimeout, ok := ctx.Value(WorkloadTimeoutStashKey{}).(string)
				if ok && stashedTimeout != "" {
					if parsedTimeout, err := time.ParseDuration(stashedTimeout); err == nil {
						timeout = parsedTimeout
					}
				}

				if waitErr := raceWithTimeout(ctx, c, workload, timeout, shouldPrint, waitErrorForStatusChange, statusChangeWorkers); waitErr != nil && opts.Output == "" {
					return cli.SilenceError(waitErr)
				}
			}

			workers = append(workers, getReadyConditionWorker(c, workload))

			if anyTail {
				workers = append(workers, getTailWorker(c, workload, opts.TailTimestamps))
			}

			waitErr := raceWithTimeout(ctx, c, workload, opts.WaitTimeout, shouldPrint, waitErrorForReadyCondition, workers)
			if waitErr != nil && opts.Output == "" {
				return cli.SilenceError(waitErr)
			}

			// since there is a possibility that wait failed but did not return
			// make sure this prompt is printed only if there is no error
			if waitErr == nil {
				cli.PrintPrompt(shouldPrint, c.Infof, "Workload %q is ready\n\n", workload.Name)
			}
		}

		if opts.Output != "" {
			// once the workload is applied, get it as is in the cluster
			if err := c.Get(ctx, client.ObjectKey{Namespace: opts.Namespace, Name: opts.Name}, workload); err != nil {
				return err
			}
			if err := opts.OutputWorkload(c, workload); err != nil {
				return err
			}
		}
	}

	return nil
}

func (opts *WorkloadApplyOptions) IsDryRun() bool {
	return opts.DryRun
}

func NewWorkloadApplyCommand(ctx context.Context, c *cli.Config) *cobra.Command {
	opts := &WorkloadApplyOptions{}
	opts.LoadDefaults(c)

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply configuration to a new or existing workload",
		Long: strings.TrimSpace(`
Apply configuration to a new or existing workload. If the resource does not exist, it will be created.

Workload configuration options include:
- source code to build
- runtime resource limits
- environment variables
- services to bind
`),
		Example: strings.Join([]string{
			fmt.Sprintf("%s workload apply %s workload.yaml", c.Name, flags.FilePathFlagName),
		}, "\n"),
		PreRunE:           cli.ValidateE(ctx, opts),
		RunE:              cli.ExecE(ctx, c, opts),
		ValidArgsFunction: completion.SuggestWorkloadNames(ctx, c),
	}

	cli.Args(cmd,
		cli.OptionalNameArg(&opts.Name),
	)

	// Define common flags
	opts.DefineFlags(ctx, c, cmd)
	cmd.Flags().StringVar(&opts.UpdateStrategy, cli.StripDash(flags.UpdateStrategyFlagName), mergeUpdateStrategy, fmt.Sprintf("specify configuration file update strategy (supported strategies: %s, %s)", mergeUpdateStrategy, replaceUpdateStrategy))
	cmd.RegisterFlagCompletionFunc(cli.StripDash(flags.UpdateStrategyFlagName), func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{replaceUpdateStrategy, mergeUpdateStrategy}, cobra.ShellCompDirectiveNoFileComp
	})

	// Bind flags to environment variables
	opts.DefineEnvVars(ctx, c, cmd)

	return cmd
}

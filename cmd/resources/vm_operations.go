package resources

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chijiajian/zstack-cli-go/pkg/client"
	"github.com/chijiajian/zstack-cli-go/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/param"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/view"
)

type vmOperationPreview struct {
	Operation string `json:"operation" yaml:"operation"`
	Instance  string `json:"instance" yaml:"instance"`
	Target    string `json:"target,omitempty" yaml:"target,omitempty"`
	Params    any    `json:"params" yaml:"params"`
}

func operationTimeout(cmd *cobra.Command) (time.Duration, error) {
	timeout, _ := cmd.Flags().GetDuration("request-timeout")
	if timeout <= 0 {
		return 0, fmt.Errorf("--request-timeout must be positive")
	}
	return timeout, nil
}

func newVMOperationClient(cmd *cobra.Command) (context.Context, context.CancelFunc, *client.MutationClient, error) {
	timeout, err := operationTimeout(cmd)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
	cli, err := client.NewMutationClient(ctx)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	return ctx, cancel, cli, nil
}

func printVMOperationDryRun(cmd *cobra.Command, preview any) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "" || output == "text" {
		output = "yaml"
	}
	return utils.PrintDryRun(preview, output)
}

func requiredIdentifier(value, flag string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", flag)
	}
	return value, nil
}

func resolveOperationTarget(ctx context.Context, cli *client.MutationClient, kind, identifier string) (client.ResolvedResource, error) {
	return cli.Reader().Resolve(ctx, kind, identifier, client.ResolveAuto)
}

func readVMRootVolume(ctx context.Context, cli *client.MutationClient, vmUUID string) (string, error) {
	var response struct {
		Inventory   view.VmInstanceInventoryView   `json:"inventory"`
		Inventories []view.VmInstanceInventoryView `json:"inventories"`
	}
	if err := cli.Reader().Read(ctx, "v1/vm-instances/"+url.PathEscape(vmUUID), nil, &response); err != nil {
		return "", err
	}
	vm := response.Inventory
	if vm.UUID == "" && len(response.Inventories) == 1 {
		vm = response.Inventories[0]
	}
	if vm.UUID != vmUUID || vm.RootVolumeUuid == "" {
		return "", fmt.Errorf("VM detail omitted matching UUID or root volume UUID")
	}
	return vm.RootVolumeUuid, nil
}

func requireVMResultIdentity(result view.VmInstanceInventoryView, expectedUUID, operation string) error {
	if result.UUID != expectedUUID {
		return fmt.Errorf("%s response omitted or mismatched VM UUID; query instance/%s before retrying", operation, expectedUUID)
	}
	return nil
}

func newInstanceMigrateCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "migrate <name-or-uuid>", Short: "Migrate a running VM to another host", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("host", "", "Destination host name or UUID")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for resolution and migration")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		host, err := requiredIdentifier(flagString(cmd, "host"), "--host")
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		request := param.MigrateVmParam{Params: param.MigrateVmParamDetail{HostUuid: &host}}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "migrate", Instance: args[0], Target: host, Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		vm, err := resolveOperationTarget(ctx, cli, "instance", args[0])
		if err != nil {
			return err
		}
		destination, err := resolveOperationTarget(ctx, cli, "host", host)
		if err != nil {
			return err
		}
		if vm.State != "Running" {
			return fmt.Errorf("instance must be Running to migrate, got %q", vm.State)
		}
		request.Params.HostUuid = &destination.UUID
		var result view.VmInstanceInventoryView
		if err := cli.Mutate(ctx, http.MethodPut, "v1/vm-instances/"+url.PathEscape(vm.UUID)+"/actions", map[string]any{"migrateVm": request.Params}, &result); err != nil {
			return err
		}
		if err := requireVMResultIdentity(result, vm.UUID, "migration"); err != nil {
			return err
		}
		return printInstanceResult(cmd, fmt.Sprintf("instance/%s migrated to host/%s", vm.Name, destination.Name), result)
	}
	return cmd
}

func newInstanceMigrateStorageCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "migrate-storage <name-or-uuid>", Short: "Migrate a VM to another primary storage", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("primary-storage", "", "Destination primary storage name or UUID")
	cmd.Flags().String("host", "", "Optional destination host name or UUID")
	cmd.Flags().Bool("with-data-volumes", false, "Migrate attached data volumes")
	cmd.Flags().Bool("with-snapshots", false, "Include volume snapshots")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for resolution and migration")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		storage, err := requiredIdentifier(flagString(cmd, "primary-storage"), "--primary-storage")
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		request := param.PrimaryStorageMigrateVmParam{Params: param.PrimaryStorageMigrateVmParamDetail{DstPrimaryStorageUuid: storage}}
		host := strings.TrimSpace(flagString(cmd, "host"))
		if host != "" {
			request.Params.DstHostUuid = &host
		}
		if cmd.Flags().Changed("with-data-volumes") {
			value, _ := cmd.Flags().GetBool("with-data-volumes")
			request.Params.WithDataVolumes = &value
		}
		if cmd.Flags().Changed("with-snapshots") {
			value, _ := cmd.Flags().GetBool("with-snapshots")
			request.Params.WithSnapshots = &value
		}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "migrate-storage", Instance: args[0], Target: storage, Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		vm, err := resolveOperationTarget(ctx, cli, "instance", args[0])
		if err != nil {
			return err
		}
		destination, err := resolveOperationTarget(ctx, cli, "primary-storage", storage)
		if err != nil {
			return err
		}
		request.Params.DstPrimaryStorageUuid = destination.UUID
		if host != "" {
			destinationHost, err := resolveOperationTarget(ctx, cli, "host", host)
			if err != nil {
				return err
			}
			request.Params.DstHostUuid = &destinationHost.UUID
		}
		var result view.VmInstanceInventoryView
		if err := cli.Mutate(ctx, http.MethodPut, "v1/vm-instances/"+url.PathEscape(vm.UUID)+"/actions", map[string]any{"primaryStorageMigrateVm": request.Params}, &result); err != nil {
			return err
		}
		if err := requireVMResultIdentity(result, vm.UUID, "storage migration"); err != nil {
			return err
		}
		return printInstanceResult(cmd, fmt.Sprintf("instance/%s migrated to primary-storage/%s", vm.Name, destination.Name), result)
	}
	return cmd
}

func newInstanceCloneCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "clone <name-or-uuid>", Short: "Clone a VM", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "Name of the new VM")
	cmd.Flags().Bool("full", false, "Include attached data volumes in the clone")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for resolution and clone")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name, err := requiredIdentifier(flagString(cmd, "name"), "--name")
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		request := param.CloneVmInstanceParam{Params: param.CloneVmInstanceParamDetail{Names: []string{name}}}
		if cmd.Flags().Changed("full") {
			value, _ := cmd.Flags().GetBool("full")
			request.Params.Full = &value
		}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "clone", Instance: args[0], Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		vm, err := resolveOperationTarget(ctx, cli, "instance", args[0])
		if err != nil {
			return err
		}
		var result view.CloneVmInstanceResultsView
		if err := cli.MutateField(ctx, http.MethodPut, "v1/vm-instances/"+url.PathEscape(vm.UUID)+"/actions", map[string]any{"cloneVmInstance": request.Params}, "result", &result); err != nil {
			return err
		}
		if result.NumberOfClonedVm != 1 || len(result.Inventories) != 1 || result.Inventories[0].Inventory.UUID == "" {
			return fmt.Errorf("clone result did not confirm one new VM; query instances before retrying")
		}
		return printInstanceResult(cmd, fmt.Sprintf("instance/%s cloned as %s", vm.Name, name), result)
	}
	return cmd
}

func flagString(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func init() {
	InstanceCmd.AddCommand(newInstanceMigrateCommand(), newInstanceMigrateStorageCommand(), newInstanceCloneCommand())
}

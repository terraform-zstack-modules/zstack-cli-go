package resources

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/chijiajian/zstack-cli-go/pkg/common"
	"github.com/spf13/cobra"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/param"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/view"
)

var zstackUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)

func requireGroupUUID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !zstackUUIDPattern.MatchString(value) {
		return "", fmt.Errorf("group UUID must contain 32 hexadecimal characters")
	}
	return strings.ToLower(value), nil
}

func newSnapshotCreateGroupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create-group <instance-name-or-uuid>", Short: "Create a VM snapshot group", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "Snapshot group name")
	cmd.Flags().StringP("description", "d", "", "Snapshot group description")
	cmd.Flags().Bool("with-memory", false, "Include VM memory")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for resolution and snapshot creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		name, err := requiredIdentifier(flagString(cmd, "name"), "--name")
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		request := param.CreateVolumeSnapshotGroupParam{Params: param.CreateVolumeSnapshotGroupParamDetail{Name: name, Description: stringPtrIfNotEmpty(flagString(cmd, "description"))}}
		if cmd.Flags().Changed("with-memory") {
			value, _ := cmd.Flags().GetBool("with-memory")
			request.Params.WithMemory = &value
		}
		if isSnapshotDryRun(cmd) {
			return printSnapshotDryRun(cmd, vmOperationPreview{Operation: "snapshot-create-group", Instance: args[0], Params: request})
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
		root, err := readVMRootVolume(ctx, cli, vm.UUID)
		if err != nil {
			return err
		}
		request.Params.RootVolumeUuid = root
		var result view.VolumeSnapshotGroupInventoryView
		if err := cli.Mutate(ctx, http.MethodPost, "v1/volume-snapshots/group", request, &result); err != nil {
			return err
		}
		if result.UUID == "" {
			return fmt.Errorf("snapshot group response omitted UUID; query snapshot groups before retrying")
		}
		return printSnapshotResult(cmd, fmt.Sprintf("snapshot-group/%s created", result.UUID), result)
	}
	return cmd
}

func newSnapshotRevertGroupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "revert-group <group-uuid>", Short: "Revert a VM from a snapshot group", Args: cobra.ExactArgs(1)}
	cmd.Flags().BoolP("yes", "y", false, "Automatic yes to confirmation prompts")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for lookup and snapshot revert")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		groupUUID, err := requireGroupUUID(args[0])
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		if isSnapshotDryRun(cmd) {
			return printSnapshotDryRun(cmd, vmOperationPreview{Operation: "snapshot-revert-group", Target: groupUUID, Params: param.RevertVmFromSnapshotGroupParam{}})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		var group struct {
			Inventory   view.VolumeSnapshotGroupInventoryView   `json:"inventory"`
			Inventories []view.VolumeSnapshotGroupInventoryView `json:"inventories"`
		}
		if err := cli.Reader().Read(ctx, "v1/volume-snapshots/group/"+url.PathEscape(groupUUID), nil, &group); err != nil {
			return err
		}
		selected := group.Inventory
		if selected.UUID == "" && len(group.Inventories) == 1 {
			selected = group.Inventories[0]
		}
		if selected.UUID != groupUUID {
			return fmt.Errorf("snapshot group lookup returned a different UUID")
		}
		confirmed, err := confirmVMRestore(cmd, fmt.Sprintf("Revert instance/%s from snapshot-group/%s? Type 'yes' to confirm: ", selected.VmInstanceUuid, groupUUID))
		if err != nil {
			return err
		}
		if !confirmed {
			return printRestoreAbort(cmd)
		}
		if err := cli.Mutate(ctx, http.MethodPut, "v1/volume-snapshots/group/"+url.PathEscape(groupUUID)+"/actions", map[string]any{"revertVmFromSnapshotGroup": param.RevertVmFromSnapshotGroupParamDetail{}}, nil); err != nil {
			return err
		}
		return printSnapshotResult(cmd, fmt.Sprintf("snapshot-group/%s reverted", groupUUID), map[string]any{"groupUuid": groupUUID, "status": "reverted"})
	}
	return cmd
}

func isSnapshotDryRun(cmd *cobra.Command) bool {
	value, _ := cmd.Flags().GetBool("dry-run")
	return value
}

func confirmVMRestore(cmd *cobra.Command, prompt string) (bool, error) {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" || output == "yaml" {
		original := cmd.OutOrStdout()
		cmd.SetOut(cmd.ErrOrStderr())
		defer cmd.SetOut(original)
	}
	return common.Confirm(cmd, prompt)
}

func printRestoreAbort(cmd *cobra.Command) error {
	output, _ := cmd.Flags().GetString("output")
	if output == "json" || output == "yaml" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted by user.")
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted by user.")
	return err
}

func init() {
	SnapshotCmd.AddCommand(newSnapshotCreateGroupCommand(), newSnapshotRevertGroupCommand())
}

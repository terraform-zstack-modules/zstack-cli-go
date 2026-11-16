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

func backupCreationFlags(cmd *cobra.Command) {
	cmd.Flags().String("backup-storage", "", "Destination backup storage name or UUID")
	cmd.Flags().String("name", "", "Backup name")
	cmd.Flags().StringP("description", "d", "", "Backup description")
	cmd.Flags().String("mode", "", "Backup mode supported by the Cloud server")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for lookup and backup creation")
}

func backupCreationInputs(cmd *cobra.Command) (string, string, string, error) {
	storage, err := requiredIdentifier(flagString(cmd, "backup-storage"), "--backup-storage")
	if err != nil {
		return "", "", "", err
	}
	name, err := requiredIdentifier(flagString(cmd, "name"), "--name")
	if err != nil {
		return "", "", "", err
	}
	if _, err = operationTimeout(cmd); err != nil {
		return "", "", "", err
	}
	mode := strings.TrimSpace(flagString(cmd, "mode"))
	if cmd.Flags().Changed("mode") && mode == "" {
		return "", "", "", fmt.Errorf("--mode must not be empty")
	}
	return storage, name, mode, nil
}

func newInstanceBackupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "backup <name-or-uuid>", Short: "Create a VM backup", Args: cobra.ExactArgs(1)}
	backupCreationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		storage, name, mode, err := backupCreationInputs(cmd)
		if err != nil {
			return err
		}
		request := param.CreateVmBackupParam{Params: param.CreateVmBackupParamDetail{BackupStorageUuid: storage, Name: name, Description: stringPtrIfNotEmpty(flagString(cmd, "description")), Mode: stringPtrIfNotEmpty(mode)}}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "vm-backup", Instance: args[0], Target: storage, Params: request})
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
		destination, err := resolveOperationTarget(ctx, cli, "backup-storage", storage)
		if err != nil {
			return err
		}
		request.Params.BackupStorageUuid = destination.UUID
		var backups []view.VolumeBackupInventoryView
		if err := cli.Mutate(ctx, http.MethodPost, "v1/volumes/"+url.PathEscape(root)+"/vm-backups", request, &backups); err != nil {
			return err
		}
		groupUUID := ""
		for _, backup := range backups {
			if backup.GroupUuid != "" {
				groupUUID = backup.GroupUuid
				break
			}
		}
		if groupUUID == "" {
			return fmt.Errorf("VM backup response omitted backup group UUID; query backups before retrying")
		}
		return printInstanceResult(cmd, fmt.Sprintf("vm-backup/%s created from instance/%s", groupUUID, vm.Name), backups)
	}
	return cmd
}

func newVolumeBackupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "backup <name-or-uuid>", Short: "Create a volume backup", Args: cobra.ExactArgs(1)}
	backupCreationFlags(cmd)
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		storage, name, mode, err := backupCreationInputs(cmd)
		if err != nil {
			return err
		}
		request := param.CreateVolumeBackupParam{Params: param.CreateVolumeBackupParamDetail{BackupStorageUuid: storage, Name: name, Description: stringPtrIfNotEmpty(flagString(cmd, "description")), Mode: stringPtrIfNotEmpty(mode)}}
		if isVolumeDryRun(cmd) {
			return printVolumeDryRun(cmd, vmOperationPreview{Operation: "volume-backup", Target: args[0], Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		volume, err := resolveOperationTarget(ctx, cli, "volume", args[0])
		if err != nil {
			return err
		}
		destination, err := resolveOperationTarget(ctx, cli, "backup-storage", storage)
		if err != nil {
			return err
		}
		request.Params.BackupStorageUuid = destination.UUID
		var result view.VolumeBackupInventoryView
		if err := cli.Mutate(ctx, http.MethodPost, "v1/volumes/"+url.PathEscape(volume.UUID)+"/volume-backups", request, &result); err != nil {
			return err
		}
		if result.UUID == "" {
			return fmt.Errorf("volume backup response omitted UUID; query backups before retrying")
		}
		return printVolumeResult(cmd, fmt.Sprintf("volume-backup/%s created from volume/%s", result.UUID, volume.Name), result)
	}
	return cmd
}

func isVolumeDryRun(cmd *cobra.Command) bool {
	value, _ := cmd.Flags().GetBool("dry-run")
	return value
}

func queryBackupGroup(ctx context.Context, cli *client.MutationClient, groupUUID string) ([]view.VolumeBackupInventoryView, error) {
	var response struct {
		Inventories []view.VolumeBackupInventoryView `json:"inventories"`
	}
	values := url.Values{"q": {"groupUuid=" + groupUUID}}
	if err := cli.Reader().Read(ctx, "v1/volume-backups", values, &response); err != nil {
		return nil, err
	}
	var matched []view.VolumeBackupInventoryView
	for _, backup := range response.Inventories {
		if backup.GroupUuid == groupUUID {
			matched = append(matched, backup)
		}
	}
	if len(matched) == 0 {
		return nil, fmt.Errorf("VM backup group %s not found", groupUUID)
	}
	return matched, nil
}

func newInstanceRevertBackupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "revert-backup <group-uuid>", Short: "Restore the original VM from a backup", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("instance", "", "Expected original VM UUID whose data will be replaced (required)")
	cmd.Flags().String("backup-storage", "", "Optional source backup storage name or UUID")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for lookup and backup restore")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		groupUUID, err := requireGroupUUID(args[0])
		if err != nil {
			return err
		}
		instanceUUID := strings.ToLower(strings.TrimSpace(flagString(cmd, "instance")))
		if !zstackUUIDPattern.MatchString(instanceUUID) {
			return fmt.Errorf("--instance must be the expected original VM UUID (32 hexadecimal characters)")
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		storage := strings.TrimSpace(flagString(cmd, "backup-storage"))
		request := param.RevertVmFromVmBackupParam{Params: param.RevertVmFromVmBackupParamDetail{BackupStorageUuid: stringPtrIfNotEmpty(storage)}}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "vm-revert-backup", Instance: instanceUUID, Target: groupUUID, Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		backups, err := queryBackupGroup(ctx, cli, groupUUID)
		if err != nil {
			return err
		}
		for _, backup := range backups {
			if backup.VmInstanceUuid == "" || !strings.EqualFold(backup.VmInstanceUuid, instanceUUID) {
				return fmt.Errorf("backup group VM identity is missing or differs from --instance; no restore submitted")
			}
		}
		vm, err := cli.Reader().ResolveExactUUID(ctx, "instance", instanceUUID)
		if err != nil {
			return err
		}
		if storage != "" {
			source, err := resolveOperationTarget(ctx, cli, "backup-storage", storage)
			if err != nil {
				return err
			}
			request.Params.BackupStorageUuid = &source.UUID
		}
		confirmed, err := confirmVMRestore(cmd, fmt.Sprintf("Replace data in instance/%s (%s) from backup-group/%s (%d matching backup records)? Type 'yes' to confirm: ", vm.UUID, vm.Name, groupUUID, len(backups)))
		if err != nil {
			return err
		}
		if !confirmed {
			return printRestoreAbort(cmd)
		}
		if err := cli.Mutate(ctx, http.MethodPut, "v1/vm-backups/"+url.PathEscape(groupUUID)+"/actions", map[string]any{"revertVmFromVmBackup": request.Params}, nil); err != nil {
			return err
		}
		return printInstanceResult(cmd, fmt.Sprintf("vm-backup/%s restored to original VM", groupUUID), map[string]any{"groupUuid": groupUUID, "status": "restored"})
	}
	return cmd
}

func newInstanceCreateFromBackupCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "create-from-backup <group-uuid>", Short: "Create a new VM from a backup", Args: cobra.ExactArgs(1)}
	cmd.Flags().String("name", "", "New VM name")
	cmd.Flags().String("backup-storage", "", "Optional source backup storage name or UUID")
	cmd.Flags().String("instance-offering", "", "Instance offering name or UUID")
	cmd.Flags().Int("cpu", 0, "CPU count instead of an instance offering (requires --memory)")
	cmd.Flags().String("memory", "", "Memory size instead of an instance offering, for example 1G (requires --cpu)")
	cmd.Flags().StringSlice("l3-network", nil, "L3 network name or UUID; repeat for multiple networks")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for lookup and VM creation")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		groupUUID, err := requireGroupUUID(args[0])
		if err != nil {
			return err
		}
		name, err := requiredIdentifier(flagString(cmd, "name"), "--name")
		if err != nil {
			return err
		}
		if _, err = operationTimeout(cmd); err != nil {
			return err
		}
		storage := strings.TrimSpace(flagString(cmd, "backup-storage"))
		offering := strings.TrimSpace(flagString(cmd, "instance-offering"))
		cpu, _ := cmd.Flags().GetInt("cpu")
		memory := strings.TrimSpace(flagString(cmd, "memory"))
		if cpu < 0 || cpu == 0 && memory != "" || cpu > 0 && memory == "" {
			return fmt.Errorf("--cpu and --memory must both be positive")
		}
		if offering != "" && (cpu > 0 || memory != "") {
			return fmt.Errorf("--instance-offering cannot be combined with --cpu or --memory")
		}
		if offering == "" && cpu == 0 {
			return fmt.Errorf("pass --instance-offering or both --cpu and --memory")
		}
		var memoryBytes int64
		if memory != "" {
			memoryBytes, err = utils.ParseMemorySize(memory)
			if err != nil || memoryBytes <= 0 {
				return fmt.Errorf("invalid --memory %q", memory)
			}
		}
		networks, _ := cmd.Flags().GetStringSlice("l3-network")
		request := param.CreateVmFromVmBackupParam{Params: param.CreateVmFromVmBackupParamDetail{Name: name, BackupStorageUuid: stringPtrIfNotEmpty(storage), InstanceOfferingUuid: stringPtrIfNotEmpty(offering), L3NetworkUuids: networks}}
		if cpu > 0 {
			request.Params.CpuNum = &cpu
			request.Params.MemorySize = &memoryBytes
		}
		if isInstanceDryRun(cmd) {
			return printVMOperationDryRun(cmd, vmOperationPreview{Operation: "vm-create-from-backup", Target: groupUUID, Params: request})
		}
		ctx, cancel, cli, err := newVMOperationClient(cmd)
		if err != nil {
			return err
		}
		defer cancel()
		if _, err := queryBackupGroup(ctx, cli, groupUUID); err != nil {
			return err
		}
		if storage != "" {
			source, err := resolveOperationTarget(ctx, cli, "backup-storage", storage)
			if err != nil {
				return err
			}
			request.Params.BackupStorageUuid = &source.UUID
		}
		if offering != "" {
			resolved, err := resolveOperationTarget(ctx, cli, "instance-offering", offering)
			if err != nil {
				return err
			}
			request.Params.InstanceOfferingUuid = &resolved.UUID
		}
		request.Params.L3NetworkUuids = nil
		for _, network := range networks {
			resolved, err := resolveOperationTarget(ctx, cli, "l3-network", network)
			if err != nil {
				return err
			}
			request.Params.L3NetworkUuids = append(request.Params.L3NetworkUuids, resolved.UUID)
		}
		var result view.VmInstanceInventoryView
		if err := cli.Mutate(ctx, http.MethodPost, "v1/vm-instances/from/vm-backups/"+url.PathEscape(groupUUID), request, &result); err != nil {
			return err
		}
		if result.UUID == "" {
			return fmt.Errorf("VM creation response omitted UUID; query instances before retrying")
		}
		return printInstanceResult(cmd, fmt.Sprintf("instance/%s created from vm-backup/%s", result.Name, groupUUID), result)
	}
	return cmd
}

func init() {
	InstanceCmd.AddCommand(newInstanceBackupCommand(), newInstanceRevertBackupCommand(), newInstanceCreateFromBackupCommand())
	VolumeCmd.AddCommand(newVolumeBackupCommand())
}

package del

import (
	"context"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chijiajian/zstack-cli-go/pkg/client"
	"github.com/chijiajian/zstack-cli-go/pkg/common"
	"github.com/chijiajian/zstack-cli-go/pkg/utils"
	"github.com/spf13/cobra"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/view"
)

func cleanupGroupUUID(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) != 32 {
		return "", fmt.Errorf("resource UUID must contain 32 hexadecimal characters")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("resource UUID must contain 32 hexadecimal characters")
	}
	return value, nil
}

func newBackupCleanupCommand(kind, path string) *cobra.Command {
	cmd := &cobra.Command{Use: kind + " <uuid>", Short: "Delete a " + kind + " by UUID", Args: cobra.ExactArgs(1)}
	cmd.Flags().BoolP("yes", "y", false, "Automatic yes to confirmation prompts")
	cmd.Flags().Duration("request-timeout", 30*time.Minute, "Deadline for lookup and deletion")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		uuid, err := cleanupGroupUUID(args[0])
		if err != nil {
			return err
		}
		timeout, _ := cmd.Flags().GetDuration("request-timeout")
		if timeout <= 0 {
			return fmt.Errorf("--request-timeout must be positive")
		}
		if dryRunFlag {
			format := outputFlag
			if format == "" {
				format = "yaml"
			}
			return utils.PrintDryRun(map[string]string{"resourceType": kind, "uuid": uuid, "action": "delete"}, format)
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		cli, err := client.NewMutationClient(ctx)
		if err != nil {
			return err
		}
		if err := confirmCleanupIdentity(ctx, cli, kind, uuid, path); err != nil {
			return err
		}
		confirmed, err := confirmCleanup(cmd, fmt.Sprintf("Delete %s/%s? Type 'yes' to confirm: ", kind, uuid))
		if err != nil {
			return err
		}
		if !confirmed {
			return printCleanupAbort(cmd)
		}
		if err := cli.Delete(ctx, path+"/"+url.PathEscape(uuid), "Permissive"); err != nil {
			return err
		}
		if outputFlag == "json" {
			return utils.Print(map[string]string{"resourceType": kind, "uuid": uuid, "status": "deleted"}, utils.JSONFormat)
		}
		_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s/%s deleted\n", kind, uuid)
		return err
	}
	return cmd
}

func confirmCleanupIdentity(ctx context.Context, cli *client.MutationClient, kind, uuid, path string) error {
	if kind == "vm-backup" {
		var response struct {
			Inventories []view.VolumeBackupInventoryView `json:"inventories"`
		}
		if err := cli.Reader().Read(ctx, "v1/volume-backups", url.Values{"q": {"groupUuid=" + uuid}}, &response); err != nil {
			return err
		}
		for _, backup := range response.Inventories {
			if backup.GroupUuid == uuid {
				return nil
			}
		}
		return fmt.Errorf("vm-backup group %s not found", uuid)
	}
	var response struct {
		Inventory struct {
			UUID string `json:"uuid"`
		} `json:"inventory"`
		Inventories []struct {
			UUID string `json:"uuid"`
		} `json:"inventories"`
	}
	if err := cli.Reader().Read(ctx, path+"/"+url.PathEscape(uuid), nil, &response); err != nil {
		return err
	}
	confirmed := response.Inventory.UUID == uuid
	if !confirmed && len(response.Inventories) == 1 {
		confirmed = response.Inventories[0].UUID == uuid
	}
	if !confirmed {
		return fmt.Errorf("%s lookup returned a different UUID", kind)
	}
	return nil
}

func confirmCleanup(cmd *cobra.Command, prompt string) (bool, error) {
	if outputFlag == "json" || outputFlag == "yaml" {
		original := cmd.OutOrStdout()
		cmd.SetOut(cmd.ErrOrStderr())
		defer cmd.SetOut(original)
	}
	return common.Confirm(cmd, prompt)
}

func printCleanupAbort(cmd *cobra.Command) error {
	if outputFlag == "json" || outputFlag == "yaml" {
		_, err := fmt.Fprintln(cmd.ErrOrStderr(), "Aborted by user.")
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Aborted by user.")
	return err
}

func init() {
	DeleteCmd.AddCommand(newBackupCleanupCommand("vm-backup", "v1/vm-backups"), newBackupCleanupCommand("volume-backup", "v1/volume-backups"), newBackupCleanupCommand("snapshot-group", "v1/volume-snapshots/group"))
}

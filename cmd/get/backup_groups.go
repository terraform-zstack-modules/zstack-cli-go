package get

import (
	"github.com/chijiajian/zstack-cli-go/pkg/common"
	"github.com/spf13/cobra"
	"github.com/zstackio/zstack-sdk-go-v2/pkg/view"
)

type formattedVolumeBackup struct {
	Name       string `json:"name" yaml:"name" header:"NAME"`
	UUID       string `json:"uuid" yaml:"uuid" header:"UUID"`
	GroupUUID  string `json:"groupUuid" yaml:"groupUuid" header:"GROUP UUID"`
	VMUUID     string `json:"vmInstanceUuid" yaml:"vmInstanceUuid" header:"VM UUID"`
	VolumeUUID string `json:"volumeUuid" yaml:"volumeUuid" header:"VOLUME UUID"`
	Status     string `json:"status" yaml:"status" header:"STATUS"`
	Mode       string `json:"mode" yaml:"mode" header:"MODE"`
}

type formattedSnapshotGroup struct {
	Name          string `json:"name" yaml:"name" header:"NAME"`
	UUID          string `json:"uuid" yaml:"uuid" header:"UUID"`
	VMUUID        string `json:"vmInstanceUuid" yaml:"vmInstanceUuid" header:"VM UUID"`
	SnapshotCount int    `json:"snapshotCount" yaml:"snapshotCount" header:"SNAPSHOTS"`
}

func newVolumeBackupsQueryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "volume-backups [name]", Short: "List VM and volume backups", Args: cobra.MaximumNArgs(1)}
	common.AddQueryFlags(cmd)
	cmd.Flags().Bool("pagination", false, "Include total count when querying backups")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		queryParam, err := common.BuildQueryParams(cmd, args, "name")
		if err != nil {
			return err
		}
		result, err := common.Fetch[view.VolumeBackupInventoryView](cmd, queryParam, "v1/volume-backups")
		if err != nil {
			return err
		}
		rows := make([]formattedVolumeBackup, 0, len(result.Items))
		for _, item := range result.Items {
			rows = append(rows, formattedVolumeBackup{Name: item.Name, UUID: item.UUID, GroupUUID: item.GroupUuid, VMUUID: item.VmInstanceUuid, VolumeUUID: item.VolumeUuid, Status: item.Status, Mode: item.Mode})
		}
		return common.PrintQuery(cmd, rows, result.Total)
	}
	return cmd
}

func newSnapshotGroupsQueryCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "snapshot-groups [name]", Short: "List VM snapshot groups", Args: cobra.MaximumNArgs(1)}
	common.AddQueryFlags(cmd)
	cmd.Flags().Bool("pagination", false, "Include total count when querying snapshot groups")
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		queryParam, err := common.BuildQueryParams(cmd, args, "name")
		if err != nil {
			return err
		}
		result, err := common.Fetch[view.VolumeSnapshotGroupInventoryView](cmd, queryParam, "v1/volume-snapshots/group")
		if err != nil {
			return err
		}
		rows := make([]formattedSnapshotGroup, 0, len(result.Items))
		for _, item := range result.Items {
			rows = append(rows, formattedSnapshotGroup{Name: item.Name, UUID: item.UUID, VMUUID: item.VmInstanceUuid, SnapshotCount: item.SnapshotCount})
		}
		return common.PrintQuery(cmd, rows, result.Total)
	}
	return cmd
}

func init() {
	GetCmd.AddCommand(newVolumeBackupsQueryCommand(), newSnapshotGroupsQueryCommand())
}

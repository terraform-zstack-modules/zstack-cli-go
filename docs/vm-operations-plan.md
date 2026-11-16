# VM operations implementation plan

[English](vm-operations-plan.md) | [简体中文](vm-operations-plan.zh-CN.md) | [Documentation index](README.md)

## Scope and command contract

This plan covers the SDK v0.0.10 VM operations missing from the CLI. It is a source-level implementation plan, not evidence of support in a particular licensed cloud.

| Workflow | Commands | SDK API |
| --- | --- | --- |
| Change compute host | `instance migrate <vm> --host <host>` | `MigrateVm` |
| Change primary storage | `instance migrate-storage <vm> --primary-storage <storage> [--host <host>] [--with-data-volumes] [--with-snapshots]` | `PrimaryStorageMigrateVm` |
| Clone VM | `instance clone <vm> --name <new-name> [--full]` (`--full` includes attached data volumes) | `CloneVmInstance` |
| VM snapshot group | `snapshot create-group <vm> --name <name> [--with-memory]`; `snapshot revert-group <group-uuid>` | `CreateVolumeSnapshotGroup`, `RevertVmFromSnapshotGroup` |
| VM backup | `instance backup <vm> --backup-storage <storage> --name <name> [--mode <mode>]` | `CreateVmBackup` |
| Volume backup | `volume backup <volume> --backup-storage <storage> --name <name> [--mode <mode>]` | `CreateVolumeBackup` |
| Restore the original VM | `instance revert-backup <group-uuid> --instance <original-vm-uuid> [--backup-storage <storage>] --yes` | `RevertVmFromVmBackup` |
| Create another VM from backup | `instance create-from-backup <group-uuid> --name <name> (--instance-offering <offering> \| --cpu <n> --memory <size>) [--l3-network <network>]...` | `CreateVmFromVmBackup` |
| Discover backups | `get volume-backups`; `get snapshot-groups` | `QueryVolumeBackup`, `QueryVolumeSnapshotGroup` |
| Clean up owned test data | `delete vm-backup <group-uuid>`; `delete volume-backup <uuid>`; `delete snapshot-group <group-uuid>` | `DeleteVmBackup`, `DeleteVolumeBackup`, `DeleteVolumeSnapshotGroup` |

For a VM backup the input is a VM identifier; the command reads its root-volume UUID before calling the SDK. Backup restore commands use a backup **group UUID**, and snapshot-group revert uses a snapshot-group UUID; they do not guess from a display name. These commands have an offline `--dry-run` request preview. Actual writes resolve all named resources exactly, reject ambiguous names, and require confirmation for actions that replace existing VM data. CLI output preserves the current text/JSON/YAML conventions. Long-running calls may return before the resulting VM or backup is Ready; users must query the returned identity and status.

## Implementation order

1. Add typed request builders and command registration for migration and clone.
2. Add snapshot-group creation/revert and backup create/query/restore commands. Reuse the SDK client and resource resolver. Add only the narrow query support required for backup and snapshot-group discovery.
3. Add regression tests for request shape, offline previews, required arguments, and confirmation behavior. Run `make verify` without cloud credentials.
4. Build the binary; regenerate `docs/reference/commands.md` and `docs/reference/commands.json`; update both usage guides, the documentation index, and capability checks; run `make docs-check` and `make skill-check`.

## Limits

Availability of hot migration, storage migration, snapshot groups, and disaster-recovery backup depends on server version, storage, and licensing. SDK v0.0.10 may not expose every newer Cloud API field; only supported typed fields are offered. A snapshot is a point-in-time copy on storage and is not a substitute for a backup stored independently.

## Implementation record

On 2026-11-14, the commands above were added to the current source tree. Local API contract tests, `make verify`, generated command reference checks, and the Skill capability check passed. The working tree contains earlier unrelated edits; this plan is not a release or deployment record.

## Live validation in `test-qa` (2026-11-14)

The user authorized a separate `test-qa` environment and newly created, dedicated resources. One test VM, one data volume, a snapshot group and two backups were created. The VM was cloned both with and without `--full`; one additional VM was created from its backup. The API detail endpoints in this environment return a one-item `inventories` array, so VM root-volume lookup and snapshot-group/backup identity checks now accept that shape as well as `inventory`.

| Operation | Observed result |
| --- | --- |
| Create/query snapshot group | Passed; matching UUID found in the query result. |
| Create/query VM backup | Passed; matching backup group and volume-backup UUID found. |
| Clone | Passed; ordinary clone had a root volume only. |
| Clone `--full` | Passed; clone had both root and attached data volume. |
| Create VM from backup | Passed; returned VM UUID and matching VM query. |
| Backup attached data volume | Passed; matching backup UUID found. An initial attempt on the same volume while detached returned HTTP 503 and created no backup. |
| Revert original VM from backup | Passed on the stopped test VM; the VM remained queryable with its root volume. Guest data contents were not inspected. |
| Migrate running VM between candidate hosts | Did not pass: Cloud returned HTTP 503; the VM remained on its original host. |
| Revert VM from snapshot group | Did not pass: Cloud returned HTTP 503; the group and stopped VM remained queryable. |
| Migrate primary storage | Not run: the test cluster had no other attached primary storage to serve as a valid destination. |

Cleanup deleted and expunged all four test VMs, the original data volume and the data volume retained by the `--full` clone. It also deleted the snapshot group, VM backup and volume backup. Final queries by recorded UUID found no remaining resource in 13 checks. The private operation journal was kept outside the repository; no credentials, server responses, resource UUIDs or network addresses were added to these docs. A local `make verify` and documentation/Skill checks after the live run are recorded separately in the final task report.

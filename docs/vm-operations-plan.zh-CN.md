# 虚拟机常见操作实施计划

[English](vm-operations-plan.md) | [简体中文](vm-operations-plan.zh-CN.md) | [文档目录](README.zh-CN.md)

## 范围与命令约定

本计划补齐项目依赖的 SDK v0.0.10 已具备、CLI 尚未暴露的虚拟机操作。它记录源码实现，不证明某个具体云环境已获许可或满足存储条件。

| 流程 | 命令 | SDK API |
| --- | --- | --- |
| 更改物理机 | `instance migrate <vm> --host <host>` | `MigrateVm` |
| 更改主存储 | `instance migrate-storage <vm> --primary-storage <storage> [--host <host>] [--with-data-volumes] [--with-snapshots]` | `PrimaryStorageMigrateVm` |
| 克隆虚拟机 | `instance clone <vm> --name <new-name> [--full]`；`--full` 包含已挂载数据盘 | `CloneVmInstance` |
| 快照组 | `snapshot create-group <vm> --name <name> [--with-memory]`；`snapshot revert-group <group-uuid>` | `CreateVolumeSnapshotGroup`、`RevertVmFromSnapshotGroup` |
| 整机备份 | `instance backup <vm> --backup-storage <storage> --name <name> [--mode <mode>]` | `CreateVmBackup` |
| 单盘备份 | `volume backup <volume> --backup-storage <storage> --name <name> [--mode <mode>]` | `CreateVolumeBackup` |
| 覆盖原虚拟机 | `instance revert-backup <group-uuid> --instance <original-vm-uuid> [--backup-storage <storage>] --yes` | `RevertVmFromVmBackup` |
| 从备份新建虚拟机 | `instance create-from-backup <group-uuid> --name <name> (--instance-offering <offering> \| --cpu <n> --memory <size>) [--l3-network <network>]...` | `CreateVmFromVmBackup` |
| 查询备份和快照组 | `get volume-backups`；`get snapshot-groups` | `QueryVolumeBackup`、`QueryVolumeSnapshotGroup` |
| 清理本轮测试资源 | `delete vm-backup <group-uuid>`；`delete volume-backup <uuid>`；`delete snapshot-group <group-uuid>` | `DeleteVmBackup`、`DeleteVolumeBackup`、`DeleteVolumeSnapshotGroup` |

整机备份以虚拟机为输入，命令先读取其根盘 UUID 再调用 SDK。恢复使用**备份组 UUID**，快照组回滚使用**快照组 UUID**，不靠显示名称猜测。新增写命令提供离线 `--dry-run`；实际执行按 UUID 或精确名称解析资源，重名时拒绝。覆盖原数据的操作要求确认。长操作返回后仍应查询目标状态，不能仅凭一次 API 返回断言虚拟机或备份已经 Ready。

## 开发顺序与验证

1. 接入迁移、主存储迁移和克隆的类型化请求与命令。
2. 接入快照组、备份创建与查询、原机恢复和从备份新建虚拟机；沿用共享认证、资源解析和写请求客户端。
3. 补请求结构、离线预览、参数验证和恢复确认的回归测试；运行 `make verify`。
4. 构建并生成 `docs/reference/commands.md`、`commands.json`；更新中英文使用指南和 Skill 能力检查；运行 `make docs-check`、`make skill-check`。

## 边界与记录

热迁移、主存储迁移、快照组与灾备备份受服务器版本、存储和许可限制。SDK v0.0.10 未必包含 Cloud 新版本的全部字段；仅暴露已核对的类型化字段。快照不是独立备份。

2026-11-14，以上命令已进入当前源码树；本地 API 契约测试、`make verify`、生成文档检查和 Skill 能力检查通过。工作区还保留此前未提交的其他修改；本记录不代表发布或部署。

## `test-qa` 真实验证（2026-11-14）

按用户授权，仅在独立 `test-qa` 环境使用本轮新建的专用资源。创建了测试 VM、数据盘、快照组和两类备份；分别做普通克隆、`--full` 克隆和从备份新建 VM。该环境的详情接口返回单元素 `inventories` 数组，因此根盘查询及快照组/备份删除前的身份检查现同时接受 `inventory` 与 `inventories`。

| 操作 | 实测结果 |
| --- | --- |
| 创建、查询快照组 | 通过；查询 UUID 匹配。 |
| 创建、查询整机备份 | 通过；备份组及备份记录 UUID 匹配。 |
| 普通克隆 | 通过；克隆 VM 只有根盘。 |
| `--full` 克隆 | 通过；克隆 VM 同时有根盘与已挂载数据盘。 |
| 从整机备份新建 VM | 通过；返回 UUID 与查询结果一致。 |
| 已挂载数据盘的单盘备份 | 通过；查询 UUID 匹配。同一数据盘未挂载时先收到 HTTP 503，核查确认未生成备份。 |
| 整机备份回滚原 VM | 已停止的测试 VM 上通过；回滚后 VM 和根盘仍可查询。未检查客户机内部数据。 |
| 候选物理机之间的热迁移 | 未通过；Cloud 返回 HTTP 503，VM 仍在原物理机。 |
| 快照组回滚 | 未通过；Cloud 返回 HTTP 503，快照组与已停止 VM 仍可查询。 |
| 主存储迁移 | 未执行；测试集群没有可用的第二个已挂载主存储。 |

清理时删除并彻底清理了四台测试 VM、原数据盘和 `--full` 克隆遗留的数据盘，也删除了快照组及两类备份。最终按已记录 UUID 做 13 项查询，全部确认不存在。私有操作 journal 保存在仓库外；文档不包含凭据、原始响应、资源 UUID 或网络地址。本轮结束后的本地 `make verify`、文档和 Skill 检查在最终交付中单独报告。

# 使用 ZStack Packer 验收 Windows PowerShell

[English](README.md) | [简体中文](README.zh-CN.md) | [文档目录](../../../docs/README.zh-CN.md)

Packer 模板 `windows-qa.pkr.hcl` 使用 ZStack 插件 1.2.1 的 `zstack-iso` builder：导入 Windows 安装介质，创建临时虚拟机和空白根盘，安装 Windows，从硬盘重启后运行离线 PowerShell QA 包，并把 `contract-report.json` 与 `qa.log` 下载到指定的本地目录。

## 构建输入

- 只使用已授权的隔离测试云。提供已核对的 L3 网络、计算规格、备份存储和根盘规格；`run_id` 必须唯一，`result_directory` 必须是已存在的空目录。如果没有可用的计算规格，可显式传入 CPU 数量和内存 MiB。
- 模板默认的 `10.255.255.15` ISO URL 是脱敏示例，实际运行时必须改为测试环境可访问的地址。若 ISO 已导入且身份已核对，可传入 `iso_image_uuid`。默认答复文件对应简体中文 Windows 10 Pro、x64、安装映像索引 4、build 18362。换用安装介质前，核对语言、版本、固件模式和安装完成标记。
- 将匹配的、已签名 Windows 10 x64 VirtIO `viostor`、`vioscsi`、`NetKVM` 驱动目录放在名为 `drivers` 的目录下，并通过 `driver_directory` 提供。答复文件预期 Windows 安装阶段的答复光盘为 `E:`；若未识别磁盘，需通过 VNC 检查盘符。首次登录脚本按卷标定位光盘。
- 备份存储需要能访问 ISO 镜像源和 Packer runner 临时发布的答复光盘。Runner 需要能访问安装期间的 VNC，以及安装后的来宾 TCP 5986。`http_ip` 是来宾和存储可达的 runner 地址；`winrm_client_address` 是来宾看到的 runner IPv4 源地址。
- 使用新的 20–64 字符 QA 密码，通过批准的秘密管理方式提供 API 和来宾凭据。来宾 WinRM 使用 HTTPS/NTLM 和临时自签名证书，防火墙仅允许指定源地址。生成的镜像只能用于隔离测试网络，后续使用前应轮换本地 QA 密码。不要把密码或带实际地址的变量文件提交到仓库。

若虚拟机进入 UEFI Shell，先检查光盘的卷映射。首次 QA 测试中，`fs0:\\efi\\boot\\bootx64.efi` 加空格启动了安装程序。确认固件映射后，可以设置 `boot_wait` 与 `boot_command` 复现。

## QA 包与结果

将 `dist/powershell/ZStack-PowerShell-Windows-QA-0.1.1.zip` 作为 `qa_kit`。该包包含 Windows CLI、测试脚本、便携版 PowerShell 7.4.20 和 7.6.6、Python 与 Pester；默认启动 7.4.20。在已有的 Windows x64 主机上，也可以解压后运行 `Run-Windows-QA.cmd`，或者运行 `Run-Windows-QA.cmd 7.6.6`。更新 CLI 或本地运行时归档后，使用 `python3 powershell/tests/build_windows_qa_kit.py --help` 查看重新打包方式。测试 API 仅监听来宾 loopback，不需要云账户。

成功时，插件在指定存储中保留新 QA 镜像而不导出，临时 VM 和导入的安装介质由插件清理。生成模板前，测试暂存目录会被删除。测试失败时，Packer 先下载可用的报告和日志，再交给 builder 清理。**验收结束后，还应清理保留的 QA 镜像及本地结果目录**，并核对云中没有遗留临时资源。

运行 `packer validate -syntax-only windows-qa.pkr.hcl` 检查语法。完整 `packer validate` 需要提供本地合成变量和驱动目录。模板校验通过不能证明云 API、存储镜像源、WinRM 可达，也不能证明 Windows 测试已成功。

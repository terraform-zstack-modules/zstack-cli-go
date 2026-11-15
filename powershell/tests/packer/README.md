# Windows PowerShell acceptance with ZStack Packer

[English](README.md) | [简体中文](README.zh-CN.md) | [Documentation index](../../../docs/README.md)

The `zstack-iso` builder in plugin 1.2.1 imports the Windows installer, creates a temporary VM with a blank root disk, installs Windows, and runs the offline PowerShell QA kit after rebooting from the hard disk. It downloads `contract-report.json` and `qa.log` to the requested local directory.

## Build inputs

- Use an authorized isolated test cloud and a verified L3 network and compute offering. Supply the backup storage and a root disk offering for the new installation. Use a unique `run_id` and an existing empty `result_directory`. If a suitable offering is unavailable, pass CPU and MiB values explicitly.
- The template's `10.255.255.15` ISO URL is an anonymized placeholder; replace it with a reachable address for a real build. Use `iso_image_uuid` if the same media is already registered and its identity has been verified. The answer file expects Windows 10 zh-CN x64 Pro at index 4, build 18362. This is a legacy compatibility target. To change media, check its language, edition index, firmware and installation marker first.
- Place the matching signed Windows 10 x64 VirtIO `viostor`, `vioscsi` and `NetKVM` driver folders under a directory named `drivers`. Pass that directory as `driver_directory` so the answer CD contains `drivers/<driver>`. The answer file expects the answer CD at `E:` during Windows Setup. Verify that assignment in VNC if setup fails to see the disk. First-logon provisioning finds the CD by volume label.
- Backup storage must reach the ISO mirror and the runner's temporary answer-CD publisher. The Packer runner must reach the VM's VNC console during setup and the guest's TCP 5986 after setup. `http_ip` is the runner's address reachable by the guest and storage. `winrm_client_address` is the runner's IPv4 source address as seen by the guest.
- Set a fresh 20–64 character QA password and supply API and guest credentials through the approved secret mechanism. The image uses WinRM HTTPS/NTLM with a self-signed guest certificate. The firewall rule allows the configured source address only. Use the resulting template in the isolated test network and rotate the local QA password before any further use. Never commit credential or environment-specific variable files.

If the VM opens UEFI Shell, inspect the optical volume mapping before choosing an EFI loader. On the first QA VM, `fs0:\efi\boot\bootx64.efi` followed by space launched Windows Setup. Set `boot_wait` and `boot_command` for a known firmware mapping when repeating the build.

## QA kit and results

Pass `dist/powershell/ZStack-PowerShell-Windows-QA-0.1.1.zip` as `qa_kit`. It contains the Windows CLI, test scripts, portable PowerShell 7.4.20 and 7.6.6, Python and Pester. The default launcher runs 7.4.20. To test on an existing Windows x64 machine, extract the kit and run `Run-Windows-QA.cmd`. Use `Run-Windows-QA.cmd 7.6.6` for the other runtime. The test API listens on guest loopback and needs no cloud account. Recreate the ZIP from an updated CLI and local runtime archives with `python3 powershell/tests/build_windows_qa_kit.py --help`.

On success, the plugin keeps a new QA image in the specified storage and skips export. The temporary VM and imported media are cleaned up by the plugin. The test staging folder is removed before templating. On a test failure, Packer downloads the available report and log before passing the failure to the builder for cleanup. After acceptance, delete the retained QA image and local results, and verify that no temporary cloud resources remain.

Use `packer validate -syntax-only windows-qa.pkr.hcl` for a syntax check. The full `packer validate` can run with synthetic local variables and the extracted drivers. A valid template does not establish API access, storage access to the mirror, WinRM reachability, or a completed Windows test.

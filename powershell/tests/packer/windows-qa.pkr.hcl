packer {
  required_plugins {
    zstack = {
      version = "= 1.2.1"
      source  = "github.com/chijiajian/zstack"
    }
  }
}

variable "zstack_host" { type = string }
variable "zstack_port" {
  type    = number
  default = 8080
}
variable "access_key_id" {
  type      = string
  sensitive = true
}
variable "access_key_secret" {
  type      = string
  sensitive = true
}
variable "root_disk_offering_uuid" { type = string }
variable "iso_url" {
  type    = string
  # Anonymized example; override with an authorized ISO URL for a real build.
  default = "http://10.255.255.15/mirror/iso/cn_windows_10.iso"
}
variable "iso_image_uuid" {
  type    = string
  default = ""
}
variable "cluster_uuid" {
  type    = string
  default = ""
}
variable "driver_directory" { type = string }
variable "http_ip" { type = string }
variable "boot_wait" {
  type    = string
  default = "0s"
}
variable "boot_command" {
  type    = list(string)
  default = []
}
variable "winrm_client_address" {
  type = string
  validation {
    condition     = can(cidrhost("${var.winrm_client_address}/32", 0))
    error_message = "WinRM client address must be a single IPv4 address."
  }
}
variable "network_uuid" { type = string }
variable "instance_offering_uuid" {
  type    = string
  default = ""
}
variable "cpu_num" {
  type    = number
  default = 8
}
variable "memory_mib" {
  type    = number
  default = 16384
}
variable "backup_storage_uuid" { type = string }
variable "winrm_password" {
  type      = string
  sensitive = true
  validation {
    condition     = can(regex("^[A-Za-z0-9!@#%+=_-]{20,64}$", var.winrm_password))
    error_message = "WinRM password must contain 20 to 64 allowed ASCII characters."
  }
}
variable "qa_kit" { type = string }
variable "result_directory" { type = string }
variable "run_id" {
  type = string
  validation {
    condition     = can(regex("^[a-z0-9][a-z0-9-]{5,39}$", var.run_id))
    error_message = "Run ID must contain 6 to 40 lowercase ASCII letters, digits or hyphens."
  }
}

locals {
  guest_directory = "C:/Windows/Temp/zstack-powershell-qa-${var.run_id}"
}

source "zstack-iso" "windows_qa" {
  zstack_host             = var.zstack_host
  port                    = var.zstack_port
  access_key_id           = var.access_key_id
  access_key_secret       = var.access_key_secret
  image_uuid              = var.iso_image_uuid
  source_image            = var.iso_image_uuid == "" ? "zstack-win10-qa-iso-${var.run_id}" : ""
  source_image_url        = var.iso_image_uuid == "" ? var.iso_url : ""
  format                  = "iso"
  root_disk_offering_uuid = var.root_disk_offering_uuid
  network_uuid            = var.network_uuid
  instance_offering_uuid  = var.instance_offering_uuid
  cpu_num                 = var.instance_offering_uuid == "" ? var.cpu_num : 0
  memory_size             = var.instance_offering_uuid == "" ? var.memory_mib : 0
  cluster_uuid            = var.cluster_uuid
  backup_storage_uuid     = var.backup_storage_uuid
  architecture            = "x86_64"
  platform                = "WindowsVirtio"
  guest_os_type           = "Windows 10 (64-bit)"
  boot_mode               = "UEFI"
  instance_name           = "zstack-powershell-qa-${var.run_id}"
  image_name              = "zstack-powershell-qa-${var.run_id}"
  skip_export             = true
  keep_image_on_error     = false

  communicator        = "winrm"
  winrm_username      = "qaadmin"
  winrm_password      = var.winrm_password
  winrm_use_ssl       = true
  winrm_use_ntlm      = true
  winrm_insecure      = true # Fresh guest uses a temporary self-signed certificate.
  winrm_timeout       = "75m"
  image_ready_timeout = "30m"
  http_ip             = var.http_ip
  boot_wait           = var.boot_wait
  boot_key_interval   = "100ms"
  boot_command        = length(var.boot_command) > 0 ? var.boot_command : [for i in range(30) : "<spacebar><wait>"]
  cd_label            = "PACKER_ANSWER"
  cd_files            = [var.driver_directory]
  cd_content = {
    "Autounattend.xml" = templatefile("Autounattend.xml.pkrtpl", {
      password       = var.winrm_password
      client_address = var.winrm_client_address
    })
    "setup.ps1" = file("Setup-WinRM.ps1")
  }
  installation_check_command = "powershell.exe -NoProfile -NonInteractive -Command \"if ((Get-CimInstance Win32_OperatingSystem).BuildNumber -ne '18362' -or -not (Test-Path 'C:/ProgramData/ZStackQA/installed')) { exit 1 }\""
}

build {
  name    = "windows-powershell-qa"
  sources = ["source.zstack-iso.windows_qa"]

  provisioner "powershell" {
    inline = [
      "$ErrorActionPreference = 'Stop'",
      "New-Item -ItemType Directory -Path '${local.guest_directory}' | Out-Null"
    ]
  }
  provisioner "file" {
    source      = var.qa_kit
    destination = "${local.guest_directory}/kit.zip"
  }
  provisioner "powershell" {
    inline = [
      "$ErrorActionPreference = 'Stop'",
      "Set-Location -LiteralPath '${local.guest_directory}'",
      "Expand-Archive -LiteralPath ./kit.zip -DestinationPath ./kit",
      "$ErrorActionPreference = 'Continue'",
      "& .\\kit\\Run-Windows-QA.cmd *> ./qa.log",
      "$result = $LASTEXITCODE",
      "$ErrorActionPreference = 'Stop'",
      "Set-Content -LiteralPath ./exit-code.txt -Value $result -Encoding ASCII",
      "if (Test-Path -LiteralPath ./kit/contract-report.json) { Copy-Item -LiteralPath ./kit/contract-report.json -Destination ./contract-report.json } else { Set-Content -LiteralPath ./contract-report.json -Value '{\"passed\":[],\"error\":\"Runner did not produce a report. See qa.log.\"}' }",
      "exit 0"
    ]
  }
  provisioner "file" {
    direction   = "download"
    source      = "${local.guest_directory}/contract-report.json"
    destination = "${var.result_directory}/contract-report.json"
  }
  provisioner "file" {
    direction   = "download"
    source      = "${local.guest_directory}/qa.log"
    destination = "${var.result_directory}/qa.log"
  }
  provisioner "powershell" {
    inline = [
      "$ErrorActionPreference = 'Stop'",
      "$result = [int](Get-Content -Raw -LiteralPath '${local.guest_directory}/exit-code.txt')",
      "Remove-Item -LiteralPath '${local.guest_directory}' -Recurse -Force",
      "if ($result -ne 0) { throw \"Windows PowerShell QA failed with exit code $result. See the downloaded qa.log.\" }"
    ]
  }
}

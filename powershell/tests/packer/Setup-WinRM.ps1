param([Parameter(Mandatory)] [ipaddress] $ClientAddress)

$ErrorActionPreference = 'Stop'
$markerDirectory = 'C:/ProgramData/ZStackQA'
$null = New-Item -ItemType Directory -Path $markerDirectory -Force
# The answer CD contains the same signed drivers used by Windows Setup.
$drivers = Join-Path $PSScriptRoot 'drivers'
$null = & pnputil.exe /add-driver (Join-Path $drivers '*.inf') /subdirs /install
if ($LASTEXITCODE -notin 0, 3010) { throw "VirtIO driver installation returned $LASTEXITCODE." }
Enable-PSRemoting -Force -SkipNetworkProfileCheck
if ([Convert]::ToBoolean((Get-Item WSMan:/localhost/Service/AllowUnencrypted).Value)) {
    throw 'WinRM must require encryption.'
}
Get-NetFirewallRule -Name 'WINRM-HTTP-In-TCP*' -ErrorAction SilentlyContinue | Disable-NetFirewallRule
Get-ChildItem WSMan:/localhost/Listener | Where-Object { $_.Keys -contains 'Transport=HTTP' } | Remove-Item -Recurse -Force
$cert = New-SelfSignedCertificate -DnsName $env:COMPUTERNAME -CertStoreLocation Cert:/LocalMachine/My
$null = New-Item -Path WSMan:/localhost/Listener -Transport HTTPS -Address '*' -CertificateThumbPrint $cert.Thumbprint -Force
$null = New-NetFirewallRule -DisplayName 'ZStack QA HTTPS WinRM' -Direction Inbound -Action Allow -Protocol TCP -LocalPort 5986 -RemoteAddress $ClientAddress.ToString() -Profile Any
Set-Service WinRM -StartupType Automatic
$null = New-ItemProperty -Path 'HKLM:/SOFTWARE/Microsoft/Windows/CurrentVersion/Policies/System' -Name LocalAccountTokenFilterPolicy -PropertyType DWord -Value 1 -Force
$null = New-Item -ItemType File -Path (Join-Path $markerDirectory 'installed') -Force

[CmdletBinding()]
param(
    [string]$BackupDirectory,

    [string]$PostgresBin = 'C:\Program Files\PostgreSQL\18\bin',

    [ValidatePattern('^(?:[01]\d|2[0-3]):[0-5]\d$')]
    [string]$DailyBackupTime = '03:30',

    [ValidateSet('Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday')]
    [string]$WeeklyVerificationDay = 'Sunday',

    [ValidatePattern('^(?:[01]\d|2[0-3]):[0-5]\d$')]
    [string]$WeeklyVerificationTime = '04:30',

    [ValidateRange(1, 3650)]
    [int]$RetentionDays = 30,

    [ValidateRange(1, 1000)]
    [int]$MinimumBackups = 7,

    [string]$TaskPath = '\Albion Market\',

    [string]$TaskUser = "$env:USERDOMAIN\$env:USERNAME",

    [System.Management.Automation.PSCredential]$Credential,

    [switch]$RunOnlyWhenLoggedOn,

    [switch]$Force
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

function ConvertTo-TaskDateTime {
    param([Parameter(Mandatory = $true)][string]$Value)

    $parts = $Value.Split(':')
    return [DateTime]::Today.AddHours([int]$parts[0]).AddMinutes([int]$parts[1])
}

function ConvertTo-CommandLineQuotedValue {
    param([Parameter(Mandatory = $true)][string]$Value)

    return '"' + $Value.Replace('"', '\"') + '"'
}

function Ensure-ScheduledTaskFolder {
    param([Parameter(Mandatory = $true)][string]$Path)

    if ($Path -eq '\') {
        return
    }

    $service = New-Object -ComObject 'Schedule.Service'
    $service.Connect()
    $currentFolder = $service.GetFolder('\')
    $segments = @($Path.Trim('\').Split('\') | Where-Object { -not [string]::IsNullOrWhiteSpace($_) })

    foreach ($segment in $segments) {
        try {
            $currentFolder = $currentFolder.GetFolder($segment)
        } catch {
            $currentFolder = $currentFolder.CreateFolder($segment, $null)
        }
    }
}

function Register-AlbionScheduledTask {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Description,
        [Parameter(Mandatory = $true)]$Action,
        [Parameter(Mandatory = $true)]$Trigger,
        [Parameter(Mandatory = $true)]$Settings,
        [string]$PlainPassword
    )

    $existing = Get-ScheduledTask -TaskName $Name -TaskPath $TaskPath -ErrorAction SilentlyContinue
    if ($null -ne $existing -and -not $Force) {
        throw "Scheduled task already exists: $TaskPath$Name. Re-run with -Force to replace it."
    }

    if ($RunOnlyWhenLoggedOn) {
        $principal = New-ScheduledTaskPrincipal `
            -UserId $TaskUser `
            -LogonType Interactive `
            -RunLevel Limited

        $task = New-ScheduledTask `
            -Action $Action `
            -Trigger $Trigger `
            -Settings $Settings `
            -Principal $principal `
            -Description $Description

        Register-ScheduledTask `
            -TaskName $Name `
            -TaskPath $TaskPath `
            -InputObject $task `
            -Force:$Force | Out-Null
    } else {
        Register-ScheduledTask `
            -TaskName $Name `
            -TaskPath $TaskPath `
            -Action $Action `
            -Trigger $Trigger `
            -Settings $Settings `
            -Description $Description `
            -User $Credential.UserName `
            -Password $PlainPassword `
            -RunLevel Limited `
            -Force:$Force | Out-Null
    }
}

Import-Module ScheduledTasks -ErrorAction Stop

if ([string]::IsNullOrWhiteSpace($BackupDirectory)) {
    $BackupDirectory = Join-Path $env:USERPROFILE 'Documents\AlbionBackups\PostgreSQL'
}
$BackupDirectory = [System.IO.Path]::GetFullPath($BackupDirectory)
$PostgresBin = [System.IO.Path]::GetFullPath($PostgresBin)

if (-not $TaskPath.StartsWith('\')) {
    $TaskPath = "\$TaskPath"
}
if (-not $TaskPath.EndsWith('\')) {
    $TaskPath = "$TaskPath\"
}

$requiredPaths = @(
    (Join-Path $repoRoot '.env.local'),
    (Join-Path $PSScriptRoot 'invoke-postgres-backup-task.ps1'),
    (Join-Path $PSScriptRoot 'invoke-postgres-restore-verification-task.ps1'),
    (Join-Path $PostgresBin 'psql.exe'),
    (Join-Path $PostgresBin 'pg_dump.exe'),
    (Join-Path $PostgresBin 'pg_restore.exe')
)
foreach ($path in $requiredPaths) {
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Required file was not found: $path"
    }
}

New-Item -ItemType Directory -Path $BackupDirectory -Force | Out-Null
Ensure-ScheduledTaskFolder -Path $TaskPath
$logDirectory = Join-Path $repoRoot 'artifacts\postgres-scheduled-tasks'
New-Item -ItemType Directory -Path $logDirectory -Force | Out-Null

if (-not $RunOnlyWhenLoggedOn -and $null -eq $Credential) {
    $Credential = Get-Credential `
        -UserName $TaskUser `
        -Message 'Windows credential used by Task Scheduler. It is not written to repository files or task arguments.'
}

$plainPassword = $null
$passwordPointer = [IntPtr]::Zero
try {
    if (-not $RunOnlyWhenLoggedOn) {
        $passwordPointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Credential.Password)
        $plainPassword = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($passwordPointer)
    }

    $powershellPath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    if (-not (Test-Path -LiteralPath $powershellPath -PathType Leaf)) {
        throw "Windows PowerShell executable was not found: $powershellPath"
    }

    $backupWrapper = Join-Path $PSScriptRoot 'invoke-postgres-backup-task.ps1'
    $verificationWrapper = Join-Path $PSScriptRoot 'invoke-postgres-restore-verification-task.ps1'

    $backupArguments = @(
        '-NoProfile'
        '-NonInteractive'
        '-ExecutionPolicy', 'Bypass'
        '-File', (ConvertTo-CommandLineQuotedValue $backupWrapper)
        '-BackupDirectory', (ConvertTo-CommandLineQuotedValue $BackupDirectory)
        '-PostgresBin', (ConvertTo-CommandLineQuotedValue $PostgresBin)
        '-RetentionDays', [string]$RetentionDays
        '-MinimumBackups', [string]$MinimumBackups
        '-LogDirectory', (ConvertTo-CommandLineQuotedValue $logDirectory)
    ) -join ' '

    $verificationArguments = @(
        '-NoProfile'
        '-NonInteractive'
        '-ExecutionPolicy', 'Bypass'
        '-File', (ConvertTo-CommandLineQuotedValue $verificationWrapper)
        '-BackupDirectory', (ConvertTo-CommandLineQuotedValue $BackupDirectory)
        '-PostgresBin', (ConvertTo-CommandLineQuotedValue $PostgresBin)
        '-LogDirectory', (ConvertTo-CommandLineQuotedValue $logDirectory)
    ) -join ' '

    $backupAction = New-ScheduledTaskAction `
        -Execute $powershellPath `
        -Argument $backupArguments `
        -WorkingDirectory $repoRoot

    $verificationAction = New-ScheduledTaskAction `
        -Execute $powershellPath `
        -Argument $verificationArguments `
        -WorkingDirectory $repoRoot

    $backupTrigger = New-ScheduledTaskTrigger `
        -Daily `
        -At (ConvertTo-TaskDateTime $DailyBackupTime)

    $verificationTrigger = New-ScheduledTaskTrigger `
        -Weekly `
        -WeeksInterval 1 `
        -DaysOfWeek $WeeklyVerificationDay `
        -At (ConvertTo-TaskDateTime $WeeklyVerificationTime)

    $backupSettings = New-ScheduledTaskSettingsSet `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -StartWhenAvailable `
        -MultipleInstances IgnoreNew `
        -ExecutionTimeLimit (New-TimeSpan -Hours 2) `
        -RestartCount 3 `
        -RestartInterval (New-TimeSpan -Minutes 15)

    $verificationSettings = New-ScheduledTaskSettingsSet `
        -AllowStartIfOnBatteries `
        -DontStopIfGoingOnBatteries `
        -StartWhenAvailable `
        -MultipleInstances IgnoreNew `
        -ExecutionTimeLimit (New-TimeSpan -Hours 4) `
        -RestartCount 2 `
        -RestartInterval (New-TimeSpan -Minutes 30)

    Register-AlbionScheduledTask `
        -Name 'PostgreSQL Backup Daily' `
        -Description 'Creates a custom-format PostgreSQL backup with SHA256 and file retention.' `
        -Action $backupAction `
        -Trigger $backupTrigger `
        -Settings $backupSettings `
        -PlainPassword $plainPassword

    Register-AlbionScheduledTask `
        -Name 'PostgreSQL Restore Verification Weekly' `
        -Description 'Restores and validates the latest PostgreSQL backup in a disposable database.' `
        -Action $verificationAction `
        -Trigger $verificationTrigger `
        -Settings $verificationSettings `
        -PlainPassword $plainPassword
} finally {
    if ($passwordPointer -ne [IntPtr]::Zero) {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($passwordPointer)
    }
    $plainPassword = $null
}

Write-Host 'PostgreSQL scheduled tasks registered successfully.'
Write-Host "TaskPath=$TaskPath"
Write-Host "DailyBackup=$DailyBackupTime"
Write-Host "WeeklyVerification=$WeeklyVerificationDay $WeeklyVerificationTime"
Write-Host "BackupDirectory=$BackupDirectory"
Write-Host "TaskUser=$TaskUser"
Write-Host "RunOnlyWhenLoggedOn=$([bool]$RunOnlyWhenLoggedOn)"
Write-Host ''
Write-Host 'Run each task once manually and then inspect its status:'
Write-Host "  Start-ScheduledTask -TaskPath '$TaskPath' -TaskName 'PostgreSQL Backup Daily'"
Write-Host "  Start-ScheduledTask -TaskPath '$TaskPath' -TaskName 'PostgreSQL Restore Verification Weekly'"
Write-Host '  .\scripts\get-postgres-backup-task-status.ps1'

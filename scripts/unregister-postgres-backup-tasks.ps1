[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'High')]
param(
    [string]$TaskPath = '\Albion Market\'
)

$ErrorActionPreference = 'Stop'
Import-Module ScheduledTasks -ErrorAction Stop

if (-not $TaskPath.StartsWith('\')) {
    $TaskPath = "\$TaskPath"
}
if (-not $TaskPath.EndsWith('\')) {
    $TaskPath = "$TaskPath\"
}

$taskNames = @(
    'PostgreSQL Retention Daily',
    'PostgreSQL Backup Daily',
    'PostgreSQL Restore Verification Weekly'
)

foreach ($taskName in $taskNames) {
    $task = Get-ScheduledTask -TaskName $taskName -TaskPath $TaskPath -ErrorAction SilentlyContinue
    if ($null -eq $task) {
        Write-Host "Scheduled task is not registered: $TaskPath$taskName"
        continue
    }

    if ($PSCmdlet.ShouldProcess("$TaskPath$taskName", 'Unregister scheduled task')) {
        Unregister-ScheduledTask -TaskName $taskName -TaskPath $TaskPath -Confirm:$false
        Write-Host "Removed scheduled task: $TaskPath$taskName"
    }
}

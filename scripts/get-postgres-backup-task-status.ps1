[CmdletBinding()]
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
    'PostgreSQL Backup Daily',
    'PostgreSQL Restore Verification Weekly'
)

$results = foreach ($taskName in $taskNames) {
    $task = Get-ScheduledTask -TaskName $taskName -TaskPath $TaskPath -ErrorAction SilentlyContinue
    if ($null -eq $task) {
        [pscustomobject]@{
            TaskName       = $taskName
            State          = 'NotRegistered'
            LastRunTime    = $null
            LastResult     = $null
            LastResultHex  = $null
            NextRunTime    = $null
        }
        continue
    }

    $info = Get-ScheduledTaskInfo -TaskName $taskName -TaskPath $TaskPath
    $lastResultUnsigned = [BitConverter]::ToUInt32(
        [BitConverter]::GetBytes([int32]$info.LastTaskResult),
        0
    )

    [pscustomobject]@{
        TaskName       = $taskName
        State          = [string]$task.State
        LastRunTime    = $info.LastRunTime
        LastResult     = $info.LastTaskResult
        LastResultHex  = ('0x{0:X8}' -f $lastResultUnsigned)
        NextRunTime    = $info.NextRunTime
    }
}

$results | Format-Table -AutoSize

$failures = @($results | Where-Object {
    $_.State -ne 'NotRegistered' -and
    $null -ne $_.LastResult -and
    $_.LastResult -ne 0 -and
    $_.LastResult -ne 267011
})

if ($failures.Count -gt 0) {
    Write-Warning 'One or more scheduled tasks have a non-zero last result. Review artifacts\postgres-scheduled-tasks\.'
}

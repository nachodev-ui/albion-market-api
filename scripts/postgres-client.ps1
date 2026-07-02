
function ConvertTo-PgPassField {
    param([AllowEmptyString()][string]$Value)

    if ($null -eq $Value) {
        return ''
    }

    return $Value.Replace('\', '\\').Replace(':', '\:')
}

function Get-PostgresClientContext {
    param([Parameter(Mandatory = $true)][string]$ConnectionString)

    try {
        $builder = [System.UriBuilder]::new($ConnectionString)
    } catch {
        throw 'The PostgreSQL connection must be a valid postgres:// or postgresql:// URL.'
    }

    if ($builder.Scheme -notin @('postgres', 'postgresql')) {
        throw 'The PostgreSQL connection must use postgres:// or postgresql://.'
    }

    $uri = $builder.Uri
    $userInfoParts = @($uri.UserInfo -split ':', 2)
    $userName = if ($userInfoParts.Count -ge 1) {
        [System.Uri]::UnescapeDataString($userInfoParts[0])
    } else {
        ''
    }
    $password = if ($userInfoParts.Count -eq 2) {
        [System.Uri]::UnescapeDataString($userInfoParts[1])
    } else {
        ''
    }
    $databaseName = [System.Uri]::UnescapeDataString($uri.AbsolutePath.TrimStart('/'))

    if ([string]::IsNullOrWhiteSpace($uri.Host)) {
        throw 'The PostgreSQL connection URL does not contain a host.'
    }
    if ([string]::IsNullOrWhiteSpace($userName)) {
        throw 'The PostgreSQL connection URL does not contain a user.'
    }
    if ([string]::IsNullOrWhiteSpace($databaseName)) {
        throw 'The PostgreSQL connection URL does not contain a database name.'
    }

    # Keep every non-secret URI option (including sslmode) while removing the
    # password from the process argument list.
    $builder.UserName = $userName
    $builder.Password = ''

    return [pscustomobject]@{
        SanitizedUrl = $builder.Uri.AbsoluteUri
        Host         = $uri.Host
        Port         = if ($uri.IsDefaultPort) { 5432 } else { $uri.Port }
        DatabaseName = $databaseName
        UserName     = $userName
        Password     = $password
    }
}

function New-TemporaryPgPassFile {
    param([Parameter(Mandatory = $true)][pscustomobject]$Context)

    $path = Join-Path ([System.IO.Path]::GetTempPath()) (
        'albion-market-api-pgpass-{0}.conf' -f [guid]::NewGuid().ToString('N')
    )
    $line = @(
        (ConvertTo-PgPassField $Context.Host)
        (ConvertTo-PgPassField ([string]$Context.Port))
        (ConvertTo-PgPassField $Context.DatabaseName)
        (ConvertTo-PgPassField $Context.UserName)
        (ConvertTo-PgPassField $Context.Password)
    ) -join ':'

    $utf8NoBom = [System.Text.UTF8Encoding]::new($false)
    [System.IO.File]::WriteAllText($path, $line + [Environment]::NewLine, $utf8NoBom)
    return $path
}

function Invoke-PostgresTool {
    param(
        [Parameter(Mandatory = $true)][string]$ToolPath,
        [string[]]$Arguments = @(),
        [string]$ConnectionString,
        [switch]$NoConnection,
        [string]$ApplicationName = 'albion-market-api-postgres-maintenance'
    )

    $hadPgPassFile = Test-Path Env:PGPASSFILE
    $previousPgPassFile = $env:PGPASSFILE
    $hadPgAppName = Test-Path Env:PGAPPNAME
    $previousPgAppName = $env:PGAPPNAME
    $temporaryPgPassFile = $null

    try {
        $effectiveArguments = @($Arguments)

        if (-not $NoConnection) {
            if ([string]::IsNullOrWhiteSpace($ConnectionString)) {
                throw 'A PostgreSQL connection URL is required for this client invocation.'
            }

            $context = Get-PostgresClientContext -ConnectionString $ConnectionString
            if (-not [string]::IsNullOrEmpty($context.Password)) {
                $temporaryPgPassFile = New-TemporaryPgPassFile -Context $context
                $env:PGPASSFILE = $temporaryPgPassFile
            }

            # --dbname is the supported place for a full connection URI. The
            # URI placed on the command line is sanitized and contains no password.
            $effectiveArguments = @('--dbname', $context.SanitizedUrl) + $effectiveArguments
        }

        $env:PGAPPNAME = $ApplicationName

        # Windows PowerShell 5.1 wraps native stderr lines (including harmless
        # PostgreSQL NOTICE messages) as non-terminating ErrorRecord objects.
        # The scripts use ErrorActionPreference=Stop, which would otherwise
        # turn a successful psql/pg_restore invocation into a terminating
        # NativeCommandError before we can inspect LASTEXITCODE.
        $previousErrorActionPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = 'Continue'
            $output = & $ToolPath @effectiveArguments 2>&1
            $exitCode = $LASTEXITCODE
        } finally {
            $ErrorActionPreference = $previousErrorActionPreference
        }

        return [pscustomobject]@{
            ExitCode = $exitCode
            Output   = @($output)
        }
    } finally {
        if ($hadPgPassFile) {
            $env:PGPASSFILE = $previousPgPassFile
        } else {
            Remove-Item Env:PGPASSFILE -ErrorAction SilentlyContinue
        }

        if ($hadPgAppName) {
            $env:PGAPPNAME = $previousPgAppName
        } else {
            Remove-Item Env:PGAPPNAME -ErrorAction SilentlyContinue
        }

        if (-not [string]::IsNullOrWhiteSpace($temporaryPgPassFile)) {
            Remove-Item -LiteralPath $temporaryPgPassFile -Force -ErrorAction SilentlyContinue
        }
    }
}

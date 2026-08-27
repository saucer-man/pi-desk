$ErrorActionPreference = "Stop"

function Invoke-GoCheck {
    param([string[]]$CommandArgs)

    $output = & go @CommandArgs 2>&1
    $exitCode = $LASTEXITCODE
    $output | ForEach-Object { Write-Output $_ }
    if ($exitCode -eq 0) {
        return
    }

    $title = "go $($CommandArgs[0]) failed"
    $output | Select-Object -Last 10 | ForEach-Object {
        $message = "$($_)".Replace("%", "%25").Replace("`r", "%0D").Replace("`n", "%0A")
        Write-Output "::error title=$title::$message"
    }
    exit $exitCode
}

Invoke-GoCheck -CommandArgs @("test", "./...")
Invoke-GoCheck -CommandArgs @("vet", "./...")

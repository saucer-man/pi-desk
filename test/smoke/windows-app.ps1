param(
    [string]$AppPath = "bin/pi-desk.exe",
    [string]$ScreenshotPath = "bin/pi-desk-smoke.png",
    [int]$TimeoutSeconds = 30
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

Add-Type -AssemblyName System.Drawing
Add-Type @"
using System;
using System.Runtime.InteropServices;

public static class PiDeskSmokeNative {
    [StructLayout(LayoutKind.Sequential)]
    public struct RECT { public int Left, Top, Right, Bottom; }

    [DllImport("user32.dll")]
    public static extern bool GetWindowRect(IntPtr handle, out RECT rect);

    [DllImport("user32.dll")]
    public static extern bool IsWindowVisible(IntPtr handle);

    [DllImport("user32.dll")]
    public static extern bool SetForegroundWindow(IntPtr handle);

    [DllImport("user32.dll")]
    public static extern bool ShowWindow(IntPtr handle, int command);

    [DllImport("user32.dll")]
    public static extern bool PrintWindow(IntPtr handle, IntPtr deviceContext, uint flags);
}
"@

$resolvedApp = (Resolve-Path -LiteralPath $AppPath).Path
$resolvedScreenshot = [System.IO.Path]::GetFullPath($ScreenshotPath)
$screenshotDirectory = Split-Path -Parent $resolvedScreenshot
[System.IO.Directory]::CreateDirectory($screenshotDirectory) | Out-Null

$process = Start-Process -FilePath $resolvedApp -PassThru
try {
    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    $handle = [IntPtr]::Zero
    while ([DateTime]::UtcNow -lt $deadline) {
        Start-Sleep -Milliseconds 250
        $process.Refresh()
        if ($process.HasExited) {
            throw "Pi Desk exited before its main window became ready (exit code $($process.ExitCode))."
        }
        $handle = $process.MainWindowHandle
        if ($handle -ne [IntPtr]::Zero -and [PiDeskSmokeNative]::IsWindowVisible($handle)) {
            break
        }
    }
    if ($handle -eq [IntPtr]::Zero) {
        throw "Pi Desk did not create a visible main window within $TimeoutSeconds seconds."
    }

    $rect = [PiDeskSmokeNative+RECT]::new()
    if (-not [PiDeskSmokeNative]::GetWindowRect($handle, [ref]$rect)) {
        throw "Unable to read the Pi Desk window bounds."
    }
    $width = $rect.Right - $rect.Left
    $height = $rect.Bottom - $rect.Top
    if ($width -lt 640 -or $height -lt 480) {
        throw "Pi Desk created an invalid window: ${width}x${height}."
    }

    [PiDeskSmokeNative]::ShowWindow($handle, 9) | Out-Null
    [PiDeskSmokeNative]::SetForegroundWindow($handle) | Out-Null
    Start-Sleep -Seconds 3

    $bitmap = [System.Drawing.Bitmap]::new($width, $height)
    $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
    try {
        $deviceContext = $graphics.GetHdc()
        try {
            if (-not [PiDeskSmokeNative]::PrintWindow($handle, $deviceContext, 2)) {
                throw "Windows could not capture the Pi Desk window."
            }
        }
        finally {
            $graphics.ReleaseHdc($deviceContext)
        }
        $bitmap.Save($resolvedScreenshot, [System.Drawing.Imaging.ImageFormat]::Png)

        $sampleColumns = 48
        $sampleRows = 32
        $brightSamples = 0
        $minimumLuminance = 255
        $maximumLuminance = 0
        for ($column = 0; $column -lt $sampleColumns; $column++) {
            $x = [Math]::Min($width - 1, [int](($column + 0.5) * $width / $sampleColumns))
            for ($row = 0; $row -lt $sampleRows; $row++) {
                $y = [Math]::Min($height - 1, [int](($row + 0.5) * $height / $sampleRows))
                $pixel = $bitmap.GetPixel($x, $y)
                $luminance = [int](0.2126 * $pixel.R + 0.7152 * $pixel.G + 0.0722 * $pixel.B)
                $minimumLuminance = [Math]::Min($minimumLuminance, $luminance)
                $maximumLuminance = [Math]::Max($maximumLuminance, $luminance)
                if ($luminance -ge 80) { $brightSamples++ }
            }
        }
        $sampleCount = $sampleColumns * $sampleRows
        if ($brightSamples -lt [Math]::Ceiling($sampleCount * 0.01) -or ($maximumLuminance - $minimumLuminance) -lt 35) {
            throw "Pi Desk window appears blank or unrendered (luminance ${minimumLuminance}-${maximumLuminance}, bright samples ${brightSamples}/${sampleCount}). Screenshot: $resolvedScreenshot"
        }
        Write-Output "Pi Desk window rendered: ${width}x${height}, luminance ${minimumLuminance}-${maximumLuminance}, screenshot $resolvedScreenshot"
    }
    finally {
        $graphics.Dispose()
        $bitmap.Dispose()
    }
}
finally {
    if (-not $process.HasExited) {
        Stop-Process -Id $process.Id -Force -ErrorAction SilentlyContinue
        $process.WaitForExit(5000) | Out-Null
    }
}

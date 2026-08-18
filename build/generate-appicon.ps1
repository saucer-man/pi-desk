param(
    [string]$OutputPath = (Join-Path $PSScriptRoot "appicon.png")
)

$ErrorActionPreference = "Stop"
Add-Type -AssemblyName System.Drawing

function New-RoundedRectanglePath {
    param(
        [float]$X,
        [float]$Y,
        [float]$Width,
        [float]$Height,
        [float]$Radius
    )

    $diameter = [Math]::Min($Radius * 2, [Math]::Min($Width, $Height))
    $path = [System.Drawing.Drawing2D.GraphicsPath]::new()
    $path.AddArc($X, $Y, $diameter, $diameter, 180, 90)
    $path.AddArc($X + $Width - $diameter, $Y, $diameter, $diameter, 270, 90)
    $path.AddArc($X + $Width - $diameter, $Y + $Height - $diameter, $diameter, $diameter, 0, 90)
    $path.AddArc($X, $Y + $Height - $diameter, $diameter, $diameter, 90, 90)
    $path.CloseFigure()
    return $path
}

$size = 1024
$bitmap = [System.Drawing.Bitmap]::new($size, $size, [System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
$bitmap.SetResolution(144, 144)
$graphics = [System.Drawing.Graphics]::FromImage($bitmap)
$graphics.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
$graphics.PixelOffsetMode = [System.Drawing.Drawing2D.PixelOffsetMode]::HighQuality
$graphics.CompositingQuality = [System.Drawing.Drawing2D.CompositingQuality]::HighQuality
$graphics.Clear([System.Drawing.Color]::Transparent)

$resources = [System.Collections.Generic.List[System.IDisposable]]::new()
try {
    $shadowPath = New-RoundedRectanglePath 48 58 928 928 204
    $shadowBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(54, 0, 0, 0))
    $resources.Add($shadowPath)
    $resources.Add($shadowBrush)
    $graphics.FillPath($shadowBrush, $shadowPath)

    $backgroundPath = New-RoundedRectanglePath 48 40 928 928 204
    $backgroundBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 31, 34, 36))
    $resources.Add($backgroundPath)
    $resources.Add($backgroundBrush)
    $graphics.FillPath($backgroundBrush, $backgroundPath)

    $borderPen = [System.Drawing.Pen]::new([System.Drawing.Color]::FromArgb(255, 76, 82, 86), 10)
    $borderPen.Alignment = [System.Drawing.Drawing2D.PenAlignment]::Inset
    $resources.Add($borderPen)
    $graphics.DrawPath($borderPen, $backgroundPath)

    $markBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 246, 247, 244))
    $resources.Add($markBrush)

    $stemPath = New-RoundedRectanglePath 244 214 186 626 78
    $resources.Add($stemPath)
    $graphics.FillPath($markBrush, $stemPath)

    $bowlPath = New-RoundedRectanglePath 320 214 520 402 190
    $resources.Add($bowlPath)
    $graphics.FillPath($markBrush, $bowlPath)

    $counterPath = New-RoundedRectanglePath 455 340 242 148 70
    $counterBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 65, 196, 126))
    $resources.Add($counterPath)
    $resources.Add($counterBrush)
    $graphics.FillPath($counterBrush, $counterPath)

    $statusRing = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 31, 34, 36))
    $statusBrush = [System.Drawing.SolidBrush]::new([System.Drawing.Color]::FromArgb(255, 255, 181, 82))
    $resources.Add($statusRing)
    $resources.Add($statusBrush)
    $graphics.FillEllipse($statusRing, 677, 683, 154, 154)
    $graphics.FillEllipse($statusBrush, 697, 703, 114, 114)

    $outputDirectory = Split-Path -Parent $OutputPath
    if ($outputDirectory) {
        [System.IO.Directory]::CreateDirectory($outputDirectory) | Out-Null
    }
    $temporaryPath = "$OutputPath.tmp.png"
    $bitmap.Save($temporaryPath, [System.Drawing.Imaging.ImageFormat]::Png)
    Move-Item -Force $temporaryPath $OutputPath
    Write-Output "Generated $OutputPath ($size x $size)"
}
finally {
    for ($index = $resources.Count - 1; $index -ge 0; $index--) {
        $resources[$index].Dispose()
    }
    $graphics.Dispose()
    $bitmap.Dispose()
}

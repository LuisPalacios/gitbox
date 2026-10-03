<#
.SYNOPSIS
  Capture the running GitboxApp window to a PNG (Windows only).

.DESCRIPTION
  Resizes the window, optionally clicks points inside it (window-relative
  pixels, e.g. to open a menu or toggle the theme), then captures it with
  PrintWindow and crops the invisible resize border Windows 11 draws
  around the client area.

.EXAMPLE
  pwsh scripts/demo-fleet/shot.ps1 -Out assets/screenshot-gui.png -Width 1400 -Height 1250
  pwsh scripts/demo-fleet/shot.ps1 -Out dark.png -Click "1190,78","1190,78"
#>
param(
  [Parameter(Mandatory)] [string]$Out,
  [int]$Width = 0,
  [int]$Height = 0,
  [string[]]$Click = @(),
  [int]$SettleMs = 1200,
  [int]$Border = 8
)
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Drawing
Add-Type @"
using System; using System.Runtime.InteropServices;
public static class GbWin {
  [StructLayout(LayoutKind.Sequential)] public struct RECT { public int L, T, R, B; }
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern bool PrintWindow(IntPtr h, IntPtr dc, uint f);
  [DllImport("user32.dll")] public static extern bool SetForegroundWindow(IntPtr h);
  [DllImport("user32.dll")] public static extern bool ShowWindow(IntPtr h, int c);
  [DllImport("user32.dll")] public static extern bool MoveWindow(IntPtr h, int x, int y, int w, int hh, bool r);
  [DllImport("user32.dll")] public static extern bool SetCursorPos(int x, int y);
  [DllImport("user32.dll")] public static extern void mouse_event(uint f, uint x, uint y, uint d, UIntPtr e);
  [DllImport("user32.dll")] public static extern bool SetProcessDPIAware();
}
"@
[GbWin]::SetProcessDPIAware() | Out-Null
$p = Get-Process GitboxApp | Where-Object { $_.MainWindowHandle -ne 0 } | Select-Object -First 1
if (-not $p) { throw 'no visible GitboxApp window — start it with run.sh first' }
$h = $p.MainWindowHandle
[GbWin]::ShowWindow($h, 9) | Out-Null
if ($Width -gt 0) { [GbWin]::MoveWindow($h, 40, 20, $Width, $Height, $true) | Out-Null }
[GbWin]::SetForegroundWindow($h) | Out-Null
Start-Sleep -Milliseconds $SettleMs

$r = New-Object GbWin+RECT
[GbWin]::GetWindowRect($h, [ref]$r) | Out-Null
foreach ($c in $Click) {
  $x, $y = $c -split ',' | ForEach-Object { [int]$_ }
  [GbWin]::SetCursorPos($r.L + $x, $r.T + $y) | Out-Null
  [GbWin]::mouse_event(0x02, 0, 0, 0, [UIntPtr]::Zero)   # left down
  [GbWin]::mouse_event(0x04, 0, 0, 0, [UIntPtr]::Zero)   # left up
  Start-Sleep -Milliseconds 700
}
if ($Click.Count -gt 0) { [GbWin]::SetCursorPos($r.R + 50, $r.B + 50) | Out-Null }
Start-Sleep -Milliseconds 400
# Clicks can resize the window (e.g. the compact view toggle): measure again.
[GbWin]::GetWindowRect($h, [ref]$r) | Out-Null

$w = $r.R - $r.L; $ht = $r.B - $r.T
$bmp = New-Object System.Drawing.Bitmap $w, $ht
$g = [System.Drawing.Graphics]::FromImage($bmp)
$dc = $g.GetHdc(); [GbWin]::PrintWindow($h, $dc, 2) | Out-Null; $g.ReleaseHdc($dc); $g.Dispose()

$crop = New-Object System.Drawing.Rectangle $Border, 0, ($w - 2 * $Border), ($ht - $Border)
$cropped = $bmp.Clone($crop, $bmp.PixelFormat)
$outPath = $ExecutionContext.SessionState.Path.GetUnresolvedProviderPathFromPSPath($Out)
$cropped.Save($outPath, [System.Drawing.Imaging.ImageFormat]::Png)
$bmp.Dispose(); $cropped.Dispose()
"saved $Out ($($w - 2 * $Border)x$($ht - $Border))"

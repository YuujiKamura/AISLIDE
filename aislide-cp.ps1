# aislide-cp.ps1 - AISLIDE control plane client (same as AICAD cad-cp.ps1, default port 9877).
# Usage: .\aislide-cp.ps1 "list signs"   |   .\aislide-cp.ps1 -Port 9877 -HostName 127.0.0.1 "state"
# Sends one UTF-8 line and prints the response (one line; for `list`, every line until the empty line).
param(
    [Parameter(Mandatory = $true, Position = 0, ValueFromRemainingArguments = $true)]
    [string[]]$Command,
    [int]$Port = 9877,
    [string]$HostName = "127.0.0.1",
    [int]$TimeoutSec = 180
)

$ErrorActionPreference = "Stop"
$line = ($Command -join " ").Trim()
if ($line -eq "") { Write-Error "empty command"; exit 2 }

$utf8 = New-Object System.Text.UTF8Encoding($false)
try { [Console]::OutputEncoding = $utf8 } catch {}

$client = New-Object System.Net.Sockets.TcpClient
try {
    $client.Connect($HostName, $Port)
    $client.ReceiveTimeout = $TimeoutSec * 1000
    $stream = $client.GetStream()
    $writer = New-Object System.IO.StreamWriter($stream, $utf8)
    $writer.NewLine = "`n"
    $writer.AutoFlush = $true
    $reader = New-Object System.IO.StreamReader($stream, $utf8)

    $writer.WriteLine($line)
    $first = $reader.ReadLine()
    if ($null -eq $first) { Write-Error "connection closed without response"; exit 1 }
    [Console]::Out.WriteLine($first)

    $verb = ($line -split '\s+')[0].ToLowerInvariant()
    if ($verb -eq "list" -and $first.StartsWith("ok")) {
        while ($true) {
            $l = $reader.ReadLine()
            if ($null -eq $l -or $l -eq "") { break }
            [Console]::Out.WriteLine($l)
        }
    }
    if ($first.StartsWith("ng")) { exit 1 }
    exit 0
}
catch {
    Write-Error ("aislide-cp: " + $_.Exception.Message)
    exit 1
}
finally {
    $client.Close()
}

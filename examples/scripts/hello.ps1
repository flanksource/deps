#!/usr/bin/env pwsh
<#
.SYNOPSIS
    Simple PowerShell script example for deps run
.DESCRIPTION
    Demonstrates deps run with PowerShell scripts
#>

param(
    [Parameter(ValueFromRemainingArguments=$true)]
    [string[]]$Arguments
)

Write-Host "Hello from PowerShell $($PSVersionTable.PSVersion)!"
Write-Host "Platform: $([System.Runtime.InteropServices.RuntimeInformation]::OSDescription)"

if ($Arguments.Count -gt 0) {
    Write-Host "Arguments: $($Arguments -join ' ')"
}

exit 0

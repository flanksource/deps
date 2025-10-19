#!/usr/bin/env pwsh
# Simple test script to verify PowerShell runtime execution

Write-Output "PowerShell Runtime Test"
Write-Output "PowerShell version: $($PSVersionTable.PSVersion)"
Write-Output "Working directory: $(Get-Location)"

# Check for environment variables
$apiKey = $env:TEST_API_KEY
if (-not $apiKey) {
    $apiKey = "not_set"
}
Write-Output "TEST_API_KEY: $apiKey"

Write-Output "Test completed successfully!"

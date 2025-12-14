# Quick local spam test for rate limiting (PowerShell)
# Usage:
#   .\scripts\loadtest.ps1 -Url "http://127.0.0.1:8080/public/hello" -N 60
param(
  [string]$Url = "http://127.0.0.1:8080/public/hello",
  [int]$N = 50
)

for ($i=1; $i -le $N; $i++) {
  try {
    $r = Invoke-WebRequest -Uri $Url -Method GET -TimeoutSec 5
    Write-Host "$i $($r.StatusCode)"
  } catch {
    if ($_.Exception.Response) {
      $code = $_.Exception.Response.StatusCode.value__
      Write-Host "$i $code"
    } else {
      Write-Host "$i error"
    }
  }
}

param([string]$Bundle=$PSScriptRoot)
$ErrorActionPreference='Stop'
$Bundle=(Resolve-Path -LiteralPath $Bundle).Path
$manifest=Get-Content -LiteralPath (Join-Path $Bundle 'manifest.json') -Raw|ConvertFrom-Json
$manifestHash=(Get-FileHash -LiteralPath (Join-Path $Bundle 'manifest.json') -Algorithm SHA256).Hash
if($manifest.format -ne 1 -or @($manifest.files).Count -eq 0){throw 'Unsupported or empty package manifest'}
$checkedPaths=@{}
function Assert-NoReparse([string]$Path){
 $current=$Path
 while($current){
  if($checkedPaths.ContainsKey($current)){break}
  if((Test-Path -LiteralPath $current) -and ((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint)){throw 'Reparse paths are not supported'}
  $checkedPaths[$current]=$true
  $current=[IO.Path]::GetDirectoryName($current)
 }
}
$listed=@{}
foreach($entry in $manifest.files){
 $path=[IO.Path]::GetFullPath((Join-Path $Bundle $entry.path))
 if(!$path.StartsWith($Bundle.TrimEnd('\')+'\',[StringComparison]::OrdinalIgnoreCase)){throw 'Invalid package path'}
 Assert-NoReparse $path
 if($listed.ContainsKey($path)){throw 'Duplicate package path'}
 $listed[$path]=$true
 if((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash -ne $entry.sha256){throw "Package checksum mismatch: $($entry.path)"}
}
Get-ChildItem -LiteralPath $Bundle -Recurse -Force -File|ForEach-Object{
 if($_.FullName -ne (Join-Path $Bundle 'manifest.json') -and !$listed.ContainsKey($_.FullName)){throw "Unlisted package file: $($_.Name)"}
}
foreach($required in @('backend/vms.exe','backend/vms-worker.exe','backend/vms-service.exe','runtime/node/node.exe','frontend/server.js')){
 if(!$listed.ContainsKey([IO.Path]::GetFullPath((Join-Path $Bundle $required)))){throw "Required package file missing: $required"}
}
$scratch=Join-Path ([IO.Path]::GetTempPath()) ('vms-package-check-'+[Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $scratch|Out-Null
$listener=[Net.Sockets.TcpListener]::new([Net.IPAddress]::Loopback,0)
$listener.Start()
$port=$listener.LocalEndpoint.Port
$listener.Stop()
$wrapper=Join-Path $Bundle 'backend/vms-service.exe'
$node=Join-Path $Bundle 'runtime/node/node.exe'
$config=@{
 executable=$node;arguments=@('server.js');directory=(Join-Path $Bundle 'frontend')
 environment=@{HOSTNAME='127.0.0.1';PORT="$port";NODE_ENV='production';NEXT_TELEMETRY_DISABLED='1'}
 log=(Join-Path $scratch 'web.log')
}
$configPath=Join-Path $scratch 'web.json'
[IO.File]::WriteAllText($configPath,($config|ConvertTo-Json -Depth 5),(New-Object Text.UTF8Encoding $false))
$supervisor=$null
function Get-OwnedNode{
 $children=@(Get-CimInstance Win32_Process -Filter "ParentProcessId=$($supervisor.Id)" |
  Where-Object{$_.ExecutablePath -eq $node -and $_.CommandLine -match 'server\.js'})
 if($children.Count -eq 1){return $children[0]}
 return $null
}
function Wait-Page{
 $deadline=(Get-Date).AddSeconds(15)
 while((Get-Date) -lt $deadline){
  if($supervisor.HasExited){throw 'Packaged supervisor exited unexpectedly'}
  try{
   $response=Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$port/" -TimeoutSec 1
   if($response.StatusCode -eq 200 -and $response.Content.Contains('VMS')){return}
  }catch{}
  Start-Sleep -Milliseconds 150
 }
 throw 'Packaged Next.js page was not available within the test window'
}
try{
 $supervisor=Start-Process -FilePath $wrapper -ArgumentList @('--console','--config',('"'+$configPath+'"')) -WorkingDirectory $scratch -WindowStyle Hidden -PassThru
 # Retain the handle, so termination cannot target a subsequently reused PID.
 [void]$supervisor.Handle
 Wait-Page
 $first=Get-OwnedNode
 if(!$first){throw 'Cannot identify the packaged child process'}
 # Kill only the exact Node child just observed under this test's supervisor.
 $child=[Diagnostics.Process]::GetProcessById($first.ProcessId)
 try{
  [void]$child.Handle
  $verified=Get-CimInstance Win32_Process -Filter "ProcessId=$($first.ProcessId)"
  if($child.HasExited -or $verified.ParentProcessId -ne $supervisor.Id -or $verified.ExecutablePath -ne $node -or $verified.CreationDate -ne $first.CreationDate){throw 'Child identity changed'}
  $child.Kill()
  if(!$child.WaitForExit(10000)){throw 'Packaged child did not stop'}
 }finally{$child.Dispose()}
 $deadline=(Get-Date).AddSeconds(15)
 $second=$null
 while((Get-Date) -lt $deadline){
  $candidate=Get-OwnedNode
  if($candidate -and $candidate.ProcessId -ne $first.ProcessId){$second=$candidate;break}
  Start-Sleep -Milliseconds 150
 }
 if(!$second){throw 'Supervisor did not restart its Node child'}
 Wait-Page
 # Also exercise the generated icon and the missing-page response.
 foreach($route in @('/icon.svg','/vms-missing-package-probe')){
  try{
   $response=Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$port$route" -TimeoutSec 5
   if($route -eq '/vms-missing-package-probe'){throw 'Expected a missing-page response'}
   if($response.StatusCode -ne 200 -or [string]$response.Headers['Content-Type'] -notmatch 'image/svg\+xml'){throw 'Packaged icon was not served'}
  }
  catch{if($route -ne '/vms-missing-package-probe' -or [int]$_.Exception.Response.StatusCode -ne 404){throw}}
 }
}finally{
 if($supervisor -and !$supervisor.HasExited){
  $supervisor.Kill()
  if(!$supervisor.WaitForExit(10000)){throw 'Packaged supervisor did not stop'}
 }
 if($supervisor){$supervisor.Dispose()}
}
# Starting the web runtime must not mutate the hash-checked release.
Get-ChildItem -LiteralPath $Bundle -Recurse -Force -File|ForEach-Object{
 if($_.FullName -ne (Join-Path $Bundle 'manifest.json') -and !$listed.ContainsKey($_.FullName)){throw "Runtime wrote into the package: $($_.Name)"}
}
foreach($entry in $manifest.files){
 if((Get-FileHash -LiteralPath (Join-Path $Bundle $entry.path) -Algorithm SHA256).Hash -ne $entry.sha256){throw "Runtime modified package file: $($entry.path)"}
}
if((Get-FileHash -LiteralPath (Join-Path $Bundle 'manifest.json') -Algorithm SHA256).Hash -ne $manifestHash){throw 'Package manifest changed during execution'}
[pscustomobject]@{manifestFiles=$manifest.files.Count;page='served';childRestarted=$true;packageUnchanged=$true;backend=$manifest.backend;frontend=$manifest.frontend;diagnostics=$scratch}|ConvertTo-Json

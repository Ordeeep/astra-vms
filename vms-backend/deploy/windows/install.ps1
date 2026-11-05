#Requires -Version 5.1
#Requires -RunAsAdministrator
param(
 [string]$Bundle=$PSScriptRoot,
 [string]$InstallRoot=(Join-Path $env:ProgramFiles 'VMS'),
 [string]$DataRoot=(Join-Path $env:ProgramData 'VMS'),
 [string]$PublicHost='localhost',
 [string]$Listen='127.0.0.1:18444',
 [int]$DatabasePort=56433,
 [int]$WebPort=13001,
 [switch]$AllowNetworkAccess
)
$ErrorActionPreference='Stop'
$Bundle=[IO.Path]::GetFullPath($Bundle)
$InstallRoot=[IO.Path]::GetFullPath($InstallRoot)
$DataRoot=[IO.Path]::GetFullPath($DataRoot)
function Invoke-Checked([string]$File,[string[]]$Arguments){
 & $File @Arguments
 if($LASTEXITCODE -ne 0){throw "Command failed: $([IO.Path]::GetFileName($File))"}
}
function New-Secret{
 $bytes=New-Object byte[] 32
 $rng=[Security.Cryptography.RandomNumberGenerator]::Create()
 try{$rng.GetBytes($bytes)}finally{$rng.Dispose()}
 return ([BitConverter]::ToString($bytes)).Replace('-','').ToLowerInvariant()
}
function Save-Json([string]$Path,$Value){[IO.File]::WriteAllText($Path,($Value|ConvertTo-Json -Depth 8),(New-Object Text.UTF8Encoding $false))}
function Assert-NoReparse([string]$Path){
 $current=$Path
 while($current){
  if(Test-Path -LiteralPath $current){
   if((Get-Item -LiteralPath $current -Force).Attributes -band [IO.FileAttributes]::ReparsePoint){throw "Reparse path is not supported: $current"}
  }
  $current=[IO.Path]::GetDirectoryName($current)
 }
}
Assert-NoReparse $InstallRoot
Assert-NoReparse $DataRoot
if($InstallRoot.TrimEnd('\') -eq $DataRoot.TrimEnd('\')){throw 'Data and release paths must differ'}
foreach($path in @($InstallRoot,$DataRoot)){
 if($path.TrimEnd('\') -eq [IO.Path]::GetPathRoot($path).TrimEnd('\')){throw 'A drive root cannot be an installation directory'}
}
$manifest=Get-Content -LiteralPath (Join-Path $Bundle 'manifest.json') -Raw|ConvertFrom-Json
if($manifest.format -ne 1){throw 'Unsupported package'}
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
$statePath=Join-Path $DataRoot 'installation.json'
$existing=Test-Path -LiteralPath $statePath
$updating=$false
if($existing){
 $state=Get-Content -LiteralPath $statePath -Raw|ConvertFrom-Json
 if($state.installRoot -ne $InstallRoot){throw 'Installation root differs from saved installation'}
 $updating=$state.status -eq 'installed'
 $PublicHost=$state.publicHost;$Listen=$state.listen;$DatabasePort=$state.databasePort;$WebPort=$state.webPort
 foreach($name in @('VMS-Core','VMS-Media','VMS-Web','VMS-Database')){
  $service=Get-CimInstance Win32_Service -Filter "Name='$name'"
  if(!$service){if($updating){throw "Installed service is missing: $name"};continue}
  $releases=if($state.managedReleases){@($state.managedReleases)}else{@($state.release)}
  $expected=if($name -eq 'VMS-Database'){@(Join-Path $InstallRoot 'postgres/bin/pg_ctl.exe')}else{@($releases|ForEach-Object{Join-Path $_ 'backend/vms-service.exe'})}
  $owned=$false
  foreach($candidate in $expected){if($service.PathName -match ('^\s*"?'+[regex]::Escape($candidate)+'"?\s')){$owned=$true}}
  if(!$owned){throw "Service does not belong to this installation: $name"}
 }
}else{
 foreach($name in @('VMS-Core','VMS-Media','VMS-Web','VMS-Database')){
  if(Get-Service -Name $name -ErrorAction SilentlyContinue){throw "Service name belongs to an existing installation: $name"}
 }
 if((Test-Path -LiteralPath $DataRoot) -and (Get-ChildItem -LiteralPath $DataRoot -Force|Select-Object -First 1)){throw 'New installation requires an empty data directory'}
 foreach($port in @($DatabasePort,$WebPort,19091,19997,18889,18554)){
  if(Get-NetTCPConnection -State Listen -LocalPort $port -ErrorAction SilentlyContinue){throw "Port already in use: $port"}
 }
 $state=[pscustomobject]@{format=1;status='preparing';installRoot=$InstallRoot;publicHost=$PublicHost;listen=$Listen;databasePort=$DatabasePort;webPort=$WebPort;databasePassword=(New-Secret);bootstrapPassword=(New-Secret);workerSecret=(New-Secret)}
}
New-Item -ItemType Directory -Path $InstallRoot,$DataRoot -Force|Out-Null
# Private installation state is readable only by Administrators, SYSTEM and
# this installation's virtual service identities (added after registration).
Invoke-Checked icacls.exe @($DataRoot,'/inheritance:r','/grant:r','*S-1-5-18:(OI)(CI)F','*S-1-5-32-544:(OI)(CI)F')
foreach($dir in @('config','logs','app','worker','postgres','backups','archive')){
 New-Item -ItemType Directory -Path (Join-Path $DataRoot $dir) -Force|Out-Null
}
$release=Join-Path $InstallRoot ('releases/'+(Get-Date -Format 'yyyyMMdd-HHmmss'))
if(Test-Path -LiteralPath $release){throw 'Release path already exists'}
New-Item -ItemType Directory -Path $release -Force|Out-Null
Get-ChildItem -LiteralPath $Bundle -Force|ForEach-Object{Copy-Item -LiteralPath $_.FullName -Destination $release -Recurse}
$state|Add-Member -NotePropertyName managedReleases -NotePropertyValue (@($state.managedReleases)+@($state.release,$release)|Where-Object{$_}|Select-Object -Unique) -Force
Save-Json $statePath $state
if(!$updating){
 $state|Add-Member -NotePropertyName release -NotePropertyValue $release -Force
 Save-Json $statePath $state
}
# Keep the database binary major version stable across application updates.
$postgres=Join-Path $InstallRoot 'postgres'
if(!(Test-Path -LiteralPath $postgres)){Copy-Item -LiteralPath (Join-Path $release 'runtime/postgres') -Destination $postgres -Recurse}
$pgData=Join-Path $DataRoot 'postgres'
$pgCtl=Join-Path $postgres 'bin/pg_ctl.exe'
if($updating){
 & (Join-Path $release 'backup.ps1') -DataRoot $DataRoot
 foreach($name in @('VMS-Core','VMS-Media','VMS-Web')){
  Stop-Service -Name $name
  (Get-Service $name).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(20))
 }
}else{
 $passwordFile=Join-Path $DataRoot 'bootstrap-password.tmp'
 try{
 if(!(Test-Path -LiteralPath (Join-Path $pgData 'PG_VERSION'))){
  [IO.File]::WriteAllText($passwordFile,$state.bootstrapPassword)
  Invoke-Checked (Join-Path $postgres 'bin/initdb.exe') @('-D',$pgData,'-U','vms_bootstrap','--encoding=UTF8','--locale=C','--auth=scram-sha-256',"--pwfile=$passwordFile")
 }
 }finally{if(Test-Path -LiteralPath $passwordFile){Remove-Item -LiteralPath $passwordFile}}
 Add-Content -LiteralPath (Join-Path $pgData 'postgresql.conf') -Value "`nlisten_addresses = '127.0.0.1'`nport = $DatabasePort`n"
 if(!(Get-Service VMS-Database -ErrorAction SilentlyContinue)){Invoke-Checked $pgCtl @('register','-N','VMS-Database','-D',$pgData,'-S','auto','-U','NT SERVICE\VMS-Database')}
 Invoke-Checked icacls.exe @($pgData,'/grant','NT SERVICE\VMS-Database:(OI)(CI)F')
 # The database identity also needs traversal of the private parent.
 Invoke-Checked icacls.exe @($DataRoot,'/grant','NT SERVICE\VMS-Database:(RX)')
 if((Get-Service VMS-Database).Status -ne 'Running'){Start-Service VMS-Database}
 $env:PGPASSWORD=$state.bootstrapPassword
 try{
  $sql=('DO $vms$ BEGIN IF NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=''vms_owner'') THEN CREATE ROLE vms_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD ''{0}''; END IF; END $vms$; GRANT CREATE ON DATABASE postgres TO vms_owner;' -f $state.databasePassword)
  $sql|& (Join-Path $postgres 'bin/psql.exe') -h 127.0.0.1 -p $DatabasePort -U vms_bootstrap -d postgres -v ON_ERROR_STOP=1
  if($LASTEXITCODE -ne 0){throw 'Database role initialization failed'}
 }finally{Remove-Item Env:PGPASSWORD}
}
$bin=Join-Path $release 'backend'
$runtime=Join-Path $release 'runtime'
$commonPath=(Join-Path $runtime 'media')+';'+(Join-Path $runtime 'node')+';'+$env:Path
$components=@{
 'VMS-Core'=@{executable=(Join-Path $bin 'vms.exe');directory=$bin;arguments=@('--data',(Join-Path $DataRoot 'app'),'--listen',$Listen,'--frontend',"http://127.0.0.1:$WebPort",'--tls-host',$PublicHost);environment=@{PATH=$commonPath;VMS_DATABASE_URL="postgres://vms_owner:$($state.databasePassword)@127.0.0.1:$DatabasePort/postgres?sslmode=disable";VMS_WORKER_SECRET=$state.workerSecret};log=(Join-Path $DataRoot 'logs/core.log')};
 'VMS-Media'=@{executable=(Join-Path $bin 'vms-worker.exe');directory=$bin;arguments=@('--mediamtx',(Join-Path $runtime 'media/mediamtx.exe'),'--data',(Join-Path $DataRoot 'worker'),'--host',$PublicHost);environment=@{PATH=$commonPath;VMS_WORKER_SECRET=$state.workerSecret};log=(Join-Path $DataRoot 'logs/media.log')};
 'VMS-Web'=@{executable=(Join-Path $runtime 'node/node.exe');directory=(Join-Path $release 'frontend');arguments=@('server.js');environment=@{PATH=$commonPath;HOSTNAME='127.0.0.1';PORT="$WebPort";NODE_ENV='production';NEXT_TELEMETRY_DISABLED='1'};log=(Join-Path $DataRoot 'logs/web.log')}
}
# Keep operator-provided certificates, listeners and other runtime options.
# Only paths owned by this package move to the new release.
function Move-ReleaseReference([string]$Value){
 foreach($old in @($state.managedReleases)){
  if($old -and $old -ne $release){$Value=$Value.Replace($old,$release)}
 }
 return $Value
}
foreach($name in @('VMS-Web','VMS-Media','VMS-Core')){
 $config=Join-Path $DataRoot "config/$name.json"
 if(Test-Path -LiteralPath $config){
  $saved=Get-Content -LiteralPath $config -Raw|ConvertFrom-Json
  $components[$name].arguments=@($saved.arguments|ForEach-Object{Move-ReleaseReference $_})
  $components[$name].directory=Move-ReleaseReference $saved.directory
  $components[$name].log=$saved.log
  $preserved=@{}
  foreach($item in $saved.environment.PSObject.Properties){$preserved[$item.Name]=Move-ReleaseReference ([string]$item.Value)}
  $components[$name].environment=$preserved
 }
 Save-Json $config $components[$name]
 $command='"'+(Join-Path $bin 'vms-service.exe')+'" --name '+$name+' --config "'+$config+'"'
 if(Get-Service $name -ErrorAction SilentlyContinue){
  if((Get-Service $name).Status -ne 'Stopped'){Stop-Service $name;(Get-Service $name).WaitForStatus('Stopped',[TimeSpan]::FromSeconds(20))}
  Invoke-Checked sc.exe @('config',$name,'binPath=',$command,'start=','auto')
 }else{
  New-Service -Name $name -BinaryPathName $command -DisplayName $name -StartupType Automatic -DependsOn @('VMS-Database')|Out-Null
 }
 Invoke-Checked sc.exe @('config',$name,'obj=',"NT SERVICE\$name")
 Invoke-Checked sc.exe @('failure',$name,'reset=','86400','actions=','restart/5000/restart/10000/restart/30000')
 Invoke-Checked sc.exe @('failureflag',$name,'1')
 Invoke-Checked icacls.exe @($DataRoot,'/grant',("NT SERVICE\{0}:(RX)" -f $name))
 Invoke-Checked icacls.exe @($config,'/grant',("NT SERVICE\{0}:(R)" -f $name))
 Invoke-Checked icacls.exe @((Join-Path $DataRoot 'logs'),'/grant',("NT SERVICE\{0}:(OI)(CI)M" -f $name))
 if($name -eq 'VMS-Core'){Invoke-Checked icacls.exe @((Join-Path $DataRoot 'app'),'/grant',("NT SERVICE\{0}:(OI)(CI)M" -f $name))}
 if($name -eq 'VMS-Media'){Invoke-Checked icacls.exe @((Join-Path $DataRoot 'worker'),'/grant',("NT SERVICE\{0}:(OI)(CI)M" -f $name))}
 if($name -eq 'VMS-Media'){Invoke-Checked icacls.exe @((Join-Path $DataRoot 'archive'),'/grant',("NT SERVICE\{0}:(OI)(CI)M" -f $name))}
}
if($updating){$state|Add-Member -NotePropertyName previousRelease -NotePropertyValue $state.release -Force}
$state|Add-Member -NotePropertyName release -NotePropertyValue $release -Force
$state|Add-Member -NotePropertyName status -NotePropertyValue 'installed' -Force
Save-Json $statePath $state
Invoke-Checked sc.exe @('failure','VMS-Database','reset=','86400','actions=','restart/5000/restart/10000/restart/30000')
if($AllowNetworkAccess){
 $publicPort=[int]($Listen.Split(':')[-1])
 if(!(Get-NetFirewallRule -Name 'VMS-HTTPS' -ErrorAction SilentlyContinue)){New-NetFirewallRule -Name 'VMS-HTTPS' -DisplayName 'VMS HTTPS' -Direction Inbound -Protocol TCP -LocalPort $publicPort -Action Allow -Profile Domain,Private|Out-Null}
 if(!(Get-NetFirewallRule -Name 'VMS-WebRTC' -ErrorAction SilentlyContinue)){New-NetFirewallRule -Name 'VMS-WebRTC' -DisplayName 'VMS WebRTC' -Direction Inbound -Protocol UDP -LocalPort 18189 -Action Allow -Profile Domain,Private|Out-Null}
}
foreach($name in @('VMS-Web','VMS-Media','VMS-Core')){Start-Service $name}
Write-Output "VMS services started. URL: https://${PublicHost}:$($Listen.Split(':')[-1])"
Write-Output 'Trust the installation certificate in the client browser, then sign in and choose the recording folder.'

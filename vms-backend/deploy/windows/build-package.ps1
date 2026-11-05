param(
 [string]$FrontendPath=(Join-Path $PSScriptRoot '../../../vms-frontend'),
 [string]$Go=(Join-Path $PSScriptRoot '../../../.tools/go/bin/go.exe'),
 [string]$PostgresRoot='C:\Program Files\PostgreSQL\16',
 [Parameter(Mandatory=$true)][string]$MediaMTX,
 [string]$Node=(Get-Command node.exe).Source,
 [string]$FFmpeg=(Get-Command ffmpeg.exe).Source,
 [string]$FFprobe=(Get-Command ffprobe.exe).Source,
 [switch]$SkipBuild
)
$ErrorActionPreference='Stop'
$backend=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '../..'))
$frontend=[IO.Path]::GetFullPath($FrontendPath)
$destination=Join-Path $backend ('dist/vms-windows-'+(Get-Date -Format 'yyyyMMdd-HHmmss'))
if(Test-Path -LiteralPath $destination){throw 'Package destination already exists'}
foreach($path in @($Go,$Node,$FFmpeg,$FFprobe,$MediaMTX,(Join-Path $PostgresRoot 'bin/pg_ctl.exe'))){
 if(!(Test-Path -LiteralPath $path -PathType Leaf)){throw "Required executable missing: $path"}
}
New-Item -ItemType Directory -Path $destination | Out-Null
if(!$SkipBuild){
 Push-Location $backend
 try{
  foreach($name in @('vms','vms-worker','vms-service')){
   & $Go build -trimpath -o "bin/$name.exe" "./cmd/$name"
   if($LASTEXITCODE -ne 0){throw 'Backend build failed'}
  }
 }finally{Pop-Location}
 Push-Location $frontend
 try{npm run build;if($LASTEXITCODE -ne 0){throw 'Frontend build failed'}}finally{Pop-Location}
}
foreach($dir in @('backend','frontend','runtime/node','runtime/media','runtime/postgres','licenses')){
 New-Item -ItemType Directory -Path (Join-Path $destination $dir) -Force|Out-Null
}
foreach($name in @('vms','vms-worker','vms-service')){
 Copy-Item -LiteralPath (Join-Path $backend "bin/$name.exe") -Destination (Join-Path $destination 'backend')
}
Get-ChildItem -LiteralPath (Join-Path $frontend '.next/standalone') -Force | ForEach-Object {
 Copy-Item -LiteralPath $_.FullName -Destination (Join-Path $destination 'frontend') -Recurse
}
Copy-Item -LiteralPath $Node -Destination (Join-Path $destination 'runtime/node/node.exe')
Copy-Item -LiteralPath $FFmpeg -Destination (Join-Path $destination 'runtime/media/ffmpeg.exe')
Copy-Item -LiteralPath $FFprobe -Destination (Join-Path $destination 'runtime/media/ffprobe.exe')
Copy-Item -LiteralPath $MediaMTX -Destination (Join-Path $destination 'runtime/media/mediamtx.exe')
foreach($dir in @('bin','lib','share')){
 Copy-Item -LiteralPath (Join-Path $PostgresRoot $dir) -Destination (Join-Path $destination 'runtime/postgres') -Recurse
}
foreach($name in @('install.ps1','backup.ps1','verify-package.ps1','README.md','THIRD-PARTY.md')){
 Copy-Item -LiteralPath (Join-Path $PSScriptRoot $name) -Destination $destination
}
$manifest=@{format=1;createdAt=(Get-Date).ToUniversalTime().ToString('o');backend=(git -C $backend rev-parse HEAD);frontend=(git -C $frontend rev-parse HEAD);files=@()}
$manifest.files=@(Get-ChildItem -LiteralPath $destination -File -Recurse -Force|ForEach-Object {
 @{path=$_.FullName.Substring($destination.Length+1).Replace('\','/');sha256=(Get-FileHash -LiteralPath $_.FullName -Algorithm SHA256).Hash;bytes=$_.Length}
})
$manifest|ConvertTo-Json -Depth 5|Set-Content -LiteralPath (Join-Path $destination 'manifest.json') -Encoding UTF8
Write-Output "Package prepared: $destination"

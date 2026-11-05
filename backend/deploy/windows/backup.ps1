#Requires -Version 5.1
#Requires -RunAsAdministrator
param([string]$DataRoot=(Join-Path $env:ProgramData 'VMS'))
$ErrorActionPreference='Stop'
$DataRoot=[IO.Path]::GetFullPath($DataRoot)
$state=Get-Content -LiteralPath (Join-Path $DataRoot 'installation.json') -Raw|ConvertFrom-Json
$destination=Join-Path $DataRoot ('backups/'+(Get-Date -Format 'yyyyMMdd-HHmmss'))
if(Test-Path -LiteralPath $destination){throw 'Backup destination already exists'}
New-Item -ItemType Directory -Path $destination -Force|Out-Null
$env:PGPASSWORD=$state.databasePassword
try{
 & (Join-Path $state.installRoot 'postgres/bin/pg_dump.exe') -h 127.0.0.1 -p $state.databasePort -U vms_owner -d postgres -n vms -Fc -f (Join-Path $destination 'catalog.dump')
 if($LASTEXITCODE -ne 0){throw 'Metadata backup failed'}
}finally{Remove-Item Env:PGPASSWORD}
foreach($name in @('installation.json','config','app','worker/archive-config.json')){
 $source=Join-Path $DataRoot $name
 if(Test-Path -LiteralPath $source){Copy-Item -LiteralPath $source -Destination $destination -Recurse}
}
@{createdAt=(Get-Date).ToUniversalTime().ToString('o');scope='metadata, keys and configuration; video files excluded';release=$state.release}|ConvertTo-Json|Set-Content -LiteralPath (Join-Path $destination 'backup.json') -Encoding UTF8
Write-Output "Metadata and installation keys backed up: $destination"

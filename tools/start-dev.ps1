$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$data = Join-Path $root '.scratch\astra-dev'
$pgBin = Join-Path $root '.tools\postgresql\pgsql\bin'
$pgData = Join-Path $data 'pgdata-local'
$pgCtl = Join-Path $pgBin 'pg_ctl.exe'
$frontend = Join-Path $root 'frontend'
$media = Join-Path $root '.tools\mediamtx\mediamtx.exe'
$processes = [ordered]@{}

function Start-Component($name, $exe, $arguments, $directory) {
    Write-Host "Iniciando $name..."
    $processes[$name] = Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $directory -PassThru -WindowStyle Hidden `
        -RedirectStandardOutput (Join-Path $data "$name.out.log") -RedirectStandardError (Join-Path $data "$name.err.log")
}

function Confirm-Components {
    foreach ($name in $processes.Keys) {
        if ($processes[$name].HasExited) { throw "$name encerrou. Consulte $data\$name.err.log" }
    }
}

function Wait-Ready($name, [scriptblock]$probe) {
    $deadline = [DateTime]::UtcNow.AddSeconds(60)
    do {
        Confirm-Components
        if (& $probe) { return }
        Start-Sleep -Milliseconds 350
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Tempo esgotado aguardando $name. Consulte os logs em $data"
}

function Test-Port([int]$port) {
    $client = New-Object Net.Sockets.TcpClient
    try { $client.Connect('127.0.0.1', $port); return $true }
    catch [Net.Sockets.SocketException] { return $false }
    finally { $client.Dispose() }
}

# This launcher operates the existing workspace database and camera credentials.
$mutex = New-Object Threading.Mutex($false, 'Local\AstraVmsDev18443')
try { $owner = $mutex.WaitOne(0) } catch [Threading.AbandonedMutexException] { $owner = $true }
if (!$owner) { throw 'O Astra Dev ja esta aberto em outra janela.' }
try {
    $node = (Get-Command node.exe).Source
    $worker = Join-Path $root 'backend\bin\vms-worker.exe'
    $collector = Join-Path $root 'backend\bin\vms-event-worker.exe'
    $core = Join-Path $root 'backend\bin\vms.exe'
    foreach ($file in @($core, $worker, $collector, $media, $pgCtl, "$pgData\PG_VERSION", "$frontend\node_modules\next\dist\bin\next")) {
        if (!(Test-Path -LiteralPath $file -PathType Leaf)) { throw "Arquivo necessario ausente: $file" }
    }
    $listeners = [Net.NetworkInformation.IPGlobalProperties]::GetIPGlobalProperties()
    foreach ($endpoint in $listeners.GetActiveTcpListeners()) {
        if ($endpoint.Port -in 13000,15432,18443,18554,18889,19091,19092,19997) { throw "Porta TCP ocupada: $($endpoint.Port)" }
    }
    if (18189 -in $listeners.GetActiveUdpListeners().Port) { throw 'Porta UDP ocupada: 18189' }
    $secret = (Get-Content "$data\worker-secret.txt" -Raw).Trim()
    $password = (Get-Content "$data\astra-db-password.txt" -Raw).Trim()
    Add-Type -Path (Join-Path $PSScriptRoot 'DevProcessGroup.cs')
    [DevProcessGroup]::Attach()
    Start-Component 'postgres' "$pgBin\postgres.exe" "-D `"$pgData`" -h 127.0.0.1 -p 15432" $data
    Wait-Ready 'PostgreSQL' { & "$pgBin\pg_isready.exe" -q -h 127.0.0.1 -p 15432; $LASTEXITCODE -eq 0 }
    [DevProcessGroup]::EnableDatabaseShutdown($pgCtl, $pgData)
    $env:NEXT_TELEMETRY_DISABLED = '1'
    Start-Component 'frontend-dev' $node 'node_modules/next/dist/bin/next dev --hostname 127.0.0.1 --port 13000' $frontend
    $env:VMS_WORKER_SECRET = $secret
    Start-Component 'worker' $worker "--mediamtx `"$media`" --data `"$data\worker`" --udp 127.0.0.1:18189" $root
    Start-Component 'event-worker' $collector "--data `"$data\events`"" $root
    Wait-Ready 'frontend, midia e coletor' { (Test-Port 13000) -and (Test-Port 19091) -and (Test-Port 19997) -and (Test-Port 18889) -and (Test-Port 18554) -and (Test-Port 19092) }
    $env:VMS_DATABASE_URL = "postgres://astra_dev:$([Uri]::EscapeDataString($password))@127.0.0.1:15432/astra_dev?sslmode=disable"
    Start-Component 'core' $core "--data `"$data\app`"" $root
    Wait-Ready 'Core' { (curl.exe -fk --silent --max-time 2 https://localhost:18443/api/v1/health) -match '"status"\s*:\s*"ok"' }
    Write-Host 'Astra disponivel em https://localhost:18443'
    Write-Host 'Feche esta janela ou pressione Ctrl+C para encerrar tudo.'
    while ($true) { Start-Sleep -Seconds 1; Confirm-Components }
} finally {
    if ($processes.Contains('postgres') -and !$processes['postgres'].HasExited) {
        & $pgCtl stop -D $pgData -m fast -w -t 5
    }
    $mutex.ReleaseMutex()
    $mutex.Dispose()
}

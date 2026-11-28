# Descarga go2rtc (el motor de vídeo) para Windows y comprueba su SHA-256 antes de dejarlo usable.
#   powershell -ExecutionPolicy Bypass -File scripts\descargar-go2rtc.ps1 [-Destino carpeta]
param([string]$Destino = ".")
$ErrorActionPreference = "Stop"

$Version = "v1.9.15"
$Asset = "go2rtc_win64.zip"
$Sha = "5b448fc90f15c925406d8da0984301985e385050222c1c2bbdf0a76fe4b1e9cb"
$Url = "https://github.com/AlexxIT/go2rtc/releases/download/$Version/$Asset"

$Tmp = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName() + ".zip")
try {
    Write-Host "Descargando $Asset ($Version)..."
    Invoke-WebRequest -Uri $Url -OutFile $Tmp -UseBasicParsing
    $Got = (Get-FileHash -Algorithm SHA256 $Tmp).Hash.ToLower()
    if ($Got -ne $Sha) {
        throw "La suma SHA-256 no coincide: esperaba $Sha y llegó $Got. No instalo nada."
    }
    New-Item -ItemType Directory -Force $Destino | Out-Null
    Expand-Archive -Path $Tmp -DestinationPath $Destino -Force
    Write-Host "go2rtc $Version comprobado e instalado en $(Join-Path $Destino 'go2rtc.exe')"
} finally {
    Remove-Item -Force -ErrorAction SilentlyContinue $Tmp
}

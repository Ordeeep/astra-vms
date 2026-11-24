@echo off
setlocal
title Astra VMS - Desenvolvimento
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0tools\start-dev.ps1"
if errorlevel 1 pause

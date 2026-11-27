@echo off
rem aislide-cp.cmd - wrapper for aislide-cp.ps1 (AISLIDE control plane, default port 9877)
rem Usage: aislide-cp "list signs"
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0aislide-cp.ps1" %*
exit /b %ERRORLEVEL%

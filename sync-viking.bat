@echo off
title Valheim Mead Hall - Viking Hero Sync
echo ==========================================================
echo  Valheim Mead Hall - Sync Your Viking
echo ==========================================================
echo Searching for your local Viking character save...
echo.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0valheim-viking-shipper.ps1"

echo.
pause

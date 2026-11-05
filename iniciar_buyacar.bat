@echo off
chcp 65001 >nul
title BuyaCAR Omnicanal — Consola de Control y Autopilot
echo ============================================================================
echo   BuyaCAR Boyaca — Iniciando Consola Web Omnicanal (SIMULATION / AUTOPILOT)
echo   Ubicacion: Av. Oriental - Cra. 6 # 52-23, frente a la Toyota, Tunja
echo ============================================================================
echo.
echo [1/2] Abriendo navegador en http://127.0.0.1:8080 ...
start "" "http://127.0.0.1:8080"
echo [2/2] Arrancando servidor Go en puerto 8080 (Modo inicial seguro: SIMULATION)...
echo       Desde la interfaz web puedes cambiar a AUTOPILOT con 1 clic.
echo.
cd /d "%~dp0app_build"
"C:\Program Files\Go\bin\go.exe" run ./cmd/buyacar --server -port 8080
pause

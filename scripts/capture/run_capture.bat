@echo off
setlocal enabledelayedexpansion

title SXD Micro-Client Packet Sniffer

cd /d "%~dp0"

echo ======================================================
echo    SXD Micro-Client Traffic Sniffer (Scapy Engine)
echo ======================================================
echo.

where python >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Python not found in PATH!
    echo Please install Python 3.8+ and add it to system PATH.
    pause
    exit /b 1
)

echo [*] Checking dependencies...
python -m pip install -r requirements.txt >nul 2>nul
if %errorlevel% neq 0 (
    echo [*] Installing requirements from requirements.txt...
    python -m pip install -r requirements.txt
)

echo.
echo [*] Launching capture_client.py...
python capture_client.py %*

if %errorlevel% neq 0 (
    echo.
    echo [!] Program exited with code %errorlevel%.
    pause
)

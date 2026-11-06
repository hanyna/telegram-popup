@echo off
taskkill /IM TelegramPopup.exe /F >nul 2>&1
echo Stopped.
timeout /t 1 >nul

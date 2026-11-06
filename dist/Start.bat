@echo off
cd /d "%~dp0"
tasklist /FI "IMAGENAME eq TelegramPopup.exe" 2>NUL | find /I "TelegramPopup.exe" >NUL
if %ERRORLEVEL%==0 (
  start "" http://127.0.0.1:8420/
  exit /b 0
)
start "" TelegramPopup.exe

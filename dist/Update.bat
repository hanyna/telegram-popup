@echo off
rem Replaces the program file only. config.json, history\, state.json and
rem telegram.session are never touched.
cd /d "%~dp0"
if not exist TelegramPopup.new (
  echo TelegramPopup.new not found next to this file.
  pause
  exit /b 1
)
taskkill /IM TelegramPopup.exe /F >nul 2>&1
timeout /t 2 >nul
if exist TelegramPopup.exe move /y TelegramPopup.exe TelegramPopup.old >nul
move /y TelegramPopup.new TelegramPopup.exe >nul
if exist TelegramPopup.exe (
  del TelegramPopup.old >nul 2>&1
  echo Updated. Starting...
  start "" TelegramPopup.exe
) else (
  move /y TelegramPopup.old TelegramPopup.exe >nul
  echo Update failed, old version restored.
  pause
)

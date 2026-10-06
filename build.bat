@echo off
setlocal
echo ========================================================
echo Building Inseyets Portable Installer
echo ========================================================

set "GOCMD=go"
where go >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    if exist "C:\Program Files\Go\bin\go.exe" (
        set "GOCMD=C:\Program Files\Go\bin\go.exe"
    ) else (
        echo [ERROR] Go compiler was not found in PATH or C:\Program Files\Go\bin\go.exe
        exit /b 1
    )
)

where go-winres.exe >nul 2>&1
if %ERRORLEVEL% NEQ 0 (
    if exist "%USERPROFILE%\go\bin\go-winres.exe" (
        "%USERPROFILE%\go\bin\go-winres.exe" make
    ) else (
        echo [ERROR] go-winres not found. Installing it now...
        "%GOCMD%" install github.com/tc-hib/go-winres@latest
        "%USERPROFILE%\go\bin\go-winres.exe" make
    )
) else (
    go-winres make
)

echo Building standalone portable executable...
"%GOCMD%" build -trimpath -ldflags="-H windowsgui" -o InseyetsInstaller.exe .

if %ERRORLEVEL% EQU 0 (
    echo.
    echo [SUCCESS] InseyetsInstaller.exe built successfully!
) else (
    echo.
    echo [ERROR] Build failed.
    exit /b 1
)

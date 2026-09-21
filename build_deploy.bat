@echo off
chcp 65001 >nul

echo ===================================================
echo [1/3] Dang build WebUI...
echo ===================================================
cd /d "%~dp0webui"
call npm run build
if %errorlevel% neq 0 (
    echo [LOI] Build WebUI that bai!
    pause
    exit /b %errorlevel%
)

cd /d "%~dp0"

echo.
echo ===================================================
echo [2/3] Dang build Go Binary cho Linux amd64...
echo ===================================================
set GOOS=linux
set GOARCH=amd64
go build -o ds2api ./cmd/ds2api
if %errorlevel% neq 0 (
    echo [LOI] Build Go Binary that bai!
    pause
    exit /b %errorlevel%
)

echo.
echo ===================================================
echo [3/3] Dang dong goi va nen file ds2api_deploy.zip...
echo ===================================================
powershell -Command "Compress-Archive -Path ds2api, static, config.json, config.example.json -DestinationPath ds2api_deploy.zip -Force"

echo.
echo ===================================================
echo HOAN THANH DONG GOI!
echo File nen tao ra: ds2api_deploy.zip
echo ===================================================
pause

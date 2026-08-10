@echo off
chcp 65001 >nul
echo ===================================================
echo [1/3] Đang build WebUI (React/Vite)...
echo ===================================================
cd webui
call npm run build
if %errorlevel% neq 0 (
    echo [LỖI] Build WebUI thất bại!
    pause
    exit /b %errorlevel%
)
cd ..

echo.
echo ===================================================
echo [2/3] Đang build Go Binary cho Linux (amd64)...
echo ===================================================
set GOOS=linux
set GOARCH=amd64
go build -o ds2api ./cmd/ds2api
if %errorlevel% neq 0 (
    echo [LỖI] Build Go Binary thất bại!
    pause
    exit /b %errorlevel%
)

echo.
echo ===================================================
echo [3/3] Đang đóng gói vào file ds2api_deploy.zip...
echo ===================================================
powershell -Command "Compress-Archive -Path ds2api, static, config.json, config.example.json -DestinationPath ds2api_deploy.zip -Force"

echo.
echo ===================================================
echo 🎉 ĐÃ HOÀN THÀNH BÓNG GÓI!
echo File nén tạo ra: ds2api_deploy.zip
echo ===================================================
pause

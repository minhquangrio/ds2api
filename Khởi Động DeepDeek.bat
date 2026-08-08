@echo off
chcp 65001 > nul
title DS2API - Start from Source

echo ===================================================
echo             DS2API - DeepSeek API Proxy
echo             (Khoi chay tu ma nguon Go)
echo ===================================================
echo.

if not exist config.json (
    if exist config.example.json (
        echo [INFO] Tao config.json tu config.example.json...
        copy config.example.json config.json > nul
    )
)

echo [INFO] Dang khoi chay: go run ./cmd/ds2api
echo [INFO] Nhan Ctrl+C de dung server.
echo ===================================================
echo.

go run ./cmd/ds2api

if errorlevel 1 (
    echo.
    echo [LOI] Chuong trinh bi dung hoac xay ra loi.
    pause
)

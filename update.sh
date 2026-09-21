#!/usr/bin/env bash
# ==============================================================================
# Script tự động cập nhật DS2API trực tiếp trên VPS aaPanel / Linux Server
# Sử dụng:
#   bash update.sh              (Cập nhật code từ Git, build lại và khởi động lại)
#   bash update.sh main         (Chỉ định nhánh Git cụ thể)
#   bash update.sh --build-only (Chỉ build lại mà không git pull)
# ==============================================================================

set -e

# Thiết lập màu hiển thị
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Xác định thư mục gốc dự án
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_DIR"

BRANCH="${1:-}"
BUILD_ONLY=false
if [ "$1" == "--build-only" ]; then
    BUILD_ONLY=true
    BRANCH=""
fi

echo -e "${PURPLE}==============================================================${NC}"
echo -e "${CYAN}        DS2API - TỰ ĐỘNG CẬP NHẬT TRÊN AAPANEL / LINUX         ${NC}"
echo -e "${PURPLE}==============================================================${NC}"
echo -e "Thư mục dự án: ${BLUE}$PROJECT_DIR${NC}"
echo -e "Thời gian: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# ------------------------------------------------------------------------------
# 1. Sao lưu cấu hình hiện tại (Bảo đảm không mất tài khoản / cài đặt)
# ------------------------------------------------------------------------------
echo -e "${BLUE}[1/5] Sao lưu dữ liệu & cấu hình...${NC}"
BACKUP_DIR="$PROJECT_DIR/backups"
TIMESTAMP=$(date '+%Y%m%d_%H%M%S')
mkdir -p "$BACKUP_DIR"

if [ -f "config.json" ]; then
    cp "config.json" "$BACKUP_DIR/config_${TIMESTAMP}.json"
    echo -e "  ${GREEN}✓${NC} Đã sao lưu config.json -> backups/config_${TIMESTAMP}.json"
fi

if [ -f ".env" ]; then
    cp ".env" "$BACKUP_DIR/env_${TIMESTAMP}.bak"
    echo -e "  ${GREEN}✓${NC} Đã sao lưu .env -> backups/env_${TIMESTAMP}.bak"
fi

# Giữ lại tối đa 10 bản sao lưu gần nhất
ls -dt "$BACKUP_DIR"/config_*.json 2>/dev/null | tail -n +11 | xargs -r rm --
ls -dt "$BACKUP_DIR"/env_*.bak 2>/dev/null | tail -n +11 | xargs -r rm --

# ------------------------------------------------------------------------------
# 2. Cập nhật mã nguồn từ Git (nếu là git repository)
# ------------------------------------------------------------------------------
if [ "$BUILD_ONLY" = false ] && [ -d ".git" ]; then
    echo ""
    echo -e "${BLUE}[2/5] Kéo mã nguồn mới nhất từ Git...${NC}"
    
    # Xác định branch hiện tại nếu không chỉ định
    if [ -z "$BRANCH" ]; then
        BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
    fi
    echo -e "  Đang ở nhánh: ${CYAN}$BRANCH${NC}"

    # Stash các file local có thể bị đè để an toàn
    git stash push -m "auto-update-stash-${TIMESTAMP}" -- config.json .env 2>/dev/null || true

    # Kéo code mới
    echo -e "  Đang kéo code mới từ origin/$BRANCH..."
    git fetch origin "$BRANCH"
    git reset --hard "origin/$BRANCH"

    # Khôi phục file cấu hình đã sao lưu
    if [ -f "$BACKUP_DIR/config_${TIMESTAMP}.json" ]; then
        cp "$BACKUP_DIR/config_${TIMESTAMP}.json" "config.json"
        echo -e "  ${GREEN}✓${NC} Đã khôi phục cài đặt config.json hiện tại"
    fi
    if [ -f "$BACKUP_DIR/env_${TIMESTAMP}.bak" ]; then
        cp "$BACKUP_DIR/env_${TIMESTAMP}.bak" ".env"
        echo -e "  ${GREEN}✓${NC} Đã khôi phục cài đặt .env hiện tại"
    fi

    LATEST_COMMIT=$(git log -1 --format="%h - %s (%cr)")
    echo -e "  ${GREEN}✓${NC} Commit mới nhất: ${YELLOW}$LATEST_COMMIT${NC}"
else
    echo ""
    echo -e "${YELLOW}[2/5] Bỏ qua git pull (chế độ build-only hoặc không có thư mục .git)${NC}"
fi

# ------------------------------------------------------------------------------
# 3. Biên dịch Frontend WebUI (nếu có Node/npm)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[3/5] Kiểm tra và build WebUI...${NC}"
if command -v npm >/dev/null 2>&1; then
    if [ -d "webui" ]; then
        echo -e "  Tìm thấy Node/NPM. Đang build WebUI..."
        cd "$PROJECT_DIR/webui"
        if [ ! -d "node_modules" ]; then
            echo -e "  Đang cài đặt thư viện npm..."
            npm ci --prefer-offline --no-audit 2>/dev/null || npm install --prefer-offline 2>/dev/null
        fi
        npm run build
        cd "$PROJECT_DIR"
        echo -e "  ${GREEN}✓${NC} WebUI đã được build vào static/admin thành công!"
    fi
else
    if [ -d "static/admin" ] && [ -f "static/admin/index.html" ]; then
        echo -e "  ${YELLOW}!${NC} Không có npm trên VPS, sử dụng bản build WebUI có sẵn trong static/admin/."
    else
        echo -e "  ${YELLOW}!${NC} Cảnh báo: Chưa tìm thấy bản build WebUI trong static/admin. Nếu giao diện admin trắng, hãy cài nodejs/npm trên VPS."
    fi
fi

# ------------------------------------------------------------------------------
# 4. Biên dịch Go Binary cho Linux
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[4/5] Biên dịch ứng dụng Go (ds2api)...${NC}"
if command -v go >/dev/null 2>&1; then
    echo -e "  Phiên bản Go: $(go version)"
    echo -e "  Đang biên dịch binary tĩnh (CGO_ENABLED=0)..."
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ds2api ./cmd/ds2api
    chmod +x ds2api
    echo -e "  ${GREEN}✓${NC} Biên dịch ds2api thành công! ($(ls -lh ds2api | awk '{print $5}'))"
else
    if [ -f "ds2api" ] && [ -x "ds2api" ]; then
        echo -e "  ${YELLOW}!${NC} Không tìm thấy Go trên VPS, giữ nguyên binary ds2api hiện tại."
    else
        echo -e "  ${RED}✗ LỖI: Không tìm thấy trình biên dịch Go ('go') và không có binary 'ds2api'!${NC}"
        echo -e "  Vui lòng cài đặt Go trên VPS bằng lệnh:"
        echo -e "    apt-get update && apt-get install -y golang"
        echo -e "  hoặc tải Go từ https://go.dev/dl/"
        exit 1
    fi
fi

# ------------------------------------------------------------------------------
# 5. Khởi động lại dịch vụ (Tương thích aaPanel Supervisor, Systemd, hoặc PM2)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[5/5] Khởi động lại dịch vụ...${NC}"

RESTARTED=false

# Cách 1: aaPanel Supervisor Manager (supervisorctl)
if command -v supervisorctl >/dev/null 2>&1; then
    if supervisorctl status ds2api 2>/dev/null | grep -qE "RUNNING|STOPPED|FATAL"; then
        echo -e "  Phát hiện tiến trình Supervisor aaPanel [ds2api], đang khởi động lại..."
        supervisorctl restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} Supervisor: Đã khởi động lại ds2api thành công!"
    fi
fi

# Cách 2: Systemd service (systemctl)
if [ "$RESTARTED" = false ] && command -v systemctl >/dev/null 2>&1; then
    if systemctl list-unit-files | grep -q "ds2api.service"; then
        echo -e "  Phát hiện Systemd service [ds2api], đang khởi động lại..."
        systemctl restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} Systemd: Đã khởi động lại ds2api thành công!"
    fi
fi

# Cách 3: PM2 Process Manager (nếu dùng aaPanel PM2)
if [ "$RESTARTED" = false ] && command -v pm2 >/dev/null 2>&1; then
    if pm2 list | grep -q "ds2api"; then
        echo -e "  Phát hiện PM2 process [ds2api], đang khởi động lại..."
        pm2 restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} PM2: Đã khởi động lại ds2api thành công!"
    fi
fi

# Cách 4: Quản lý tiến trình độc lập (Standalone process)
if [ "$RESTARTED" = false ]; then
    echo -e "  Không phát hiện Supervisor/Systemd, đang quản lý tiến trình trực tiếp..."
    
    # Dừng tiến trình cũ nếu đang chạy
    if pgrep -f "./ds2api" >/dev/null 2>&1; then
        echo -e "  Đang dừng tiến trình ds2api cũ..."
        pkill -f "./ds2api" || true
        sleep 1
    fi
    
    # Khởi động lại ở chế độ nền
    nohup ./ds2api > ds2api.log 2>&1 &
    NEW_PID=$!
    echo -e "  ${GREEN}✓${NC} Đã khởi động ds2api mới ở chế độ nền (PID: ${CYAN}$NEW_PID${NC})"
    echo -e "  File nhật ký (log): ${BLUE}$PROJECT_DIR/ds2api.log${NC}"
    RESTARTED=true
fi

# ------------------------------------------------------------------------------
# Kiểm tra sức khỏe dịch vụ (Health Check)
# ------------------------------------------------------------------------------
echo ""
echo -e "${CYAN}Đang kiểm tra trạng thái hoạt động...${NC}"
sleep 2

# Trích xuất cổng từ config.json hoặc mặc định 5001
PORT=5001
if [ -f "config.json" ]; then
    CFG_PORT=$(grep -o '"port"[[:space:]]*:[[:space:]]*[0-9]*' config.json | grep -o '[0-9]*' || true)
    if [ -n "$CFG_PORT" ]; then
        PORT=$CFG_PORT
    fi
fi

# Thử gọi endpoint /healthz
HEALTH_CHECK=$(curl -s -m 3 "http://127.0.0.1:${PORT}/healthz" 2>/dev/null || true)
if [[ "$HEALTH_CHECK" == *"ok"* ]]; then
    echo -e "${GREEN}==============================================================${NC}"
    echo -e "${GREEN}       ✓ CẬP NHẬT & KHỞI ĐỘNG DS2API THÀNH CÔNG RỰC RỠ!      ${NC}"
    echo -e "${GREEN}==============================================================${NC}"
    echo -e "  Địa chỉ nội bộ : ${CYAN}http://127.0.0.1:${PORT}${NC}"
    echo -e "  Bảng điều khiển: ${CYAN}http://127.0.0.1:${PORT}/admin${NC}"
    echo -e "  Trạng thái     : ${GREEN}Đang hoạt động (Health: OK)${NC}"
else
    echo -e "${YELLOW}==============================================================${NC}"
    echo -e "${YELLOW}  Đã cập nhật mã nguồn xong. Hãy kiểm tra trạng thái dịch vụ!  ${NC}"
    echo -e "${YELLOW}==============================================================${NC}"
    echo -e "  Kiểm tra log bằng lệnh: ${CYAN}tail -n 50 ds2api.log${NC}"
fi

echo ""

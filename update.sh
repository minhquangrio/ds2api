#!/usr/bin/env bash
# ==============================================================================
# Script tự động cập nhật DS2API trực tiếp trên VPS aaPanel / Linux Server
# Sử dụng:
#   bash update.sh              (Tự động kết nối Git, kéo code mới và build lại)
#   bash update.sh main         (Chỉ định nhánh Git cụ thể)
#   bash update.sh main https://github.com/user/ds2api.git (Chỉ định URL Repo)
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

BRANCH="${1:-main}"
DEFAULT_REPO="https://github.com/minhquangrio/ds2api.git"
REPO_URL="${2:-$DEFAULT_REPO}"

echo -e "${PURPLE}==============================================================${NC}"
echo -e "${CYAN}        DS2API - TỰ ĐỘNG CẬP NHẬT TRÊN AAPANEL / LINUX         ${NC}"
echo -e "${PURPLE}==============================================================${NC}"
echo -e "Thư mục dự án: ${BLUE}$PROJECT_DIR${NC}"
echo -e "Thời gian: $(date '+%Y-%m-%d %H:%M:%S')"
echo ""

# ------------------------------------------------------------------------------
# 1. Sao lưu cấu hình hiện tại (Bảo đảm không bao giờ mất tài khoản / cài đặt)
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
ls -dt "$BACKUP_DIR"/config_*.json 2>/dev/null | tail -n +11 | xargs -r rm -- 2>/dev/null || true
ls -dt "$BACKUP_DIR"/env_*.bak 2>/dev/null | tail -n +11 | xargs -r rm -- 2>/dev/null || true

# ------------------------------------------------------------------------------
# 2. Khởi tạo / Cập nhật mã nguồn từ Git
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[2/5] Kéo mã nguồn mới nhất từ Git...${NC}"

# Nếu chưa có Git hoặc thiếu go.mod, tự động khởi tạo git và liên kết repo
if [ ! -d ".git" ]; then
    echo -e "  ${YELLOW}! Thư mục chưa có Git. Đang tự động kết nối với repo:${NC} ${CYAN}$REPO_URL${NC}..."
    git init
    git remote add origin "$REPO_URL" 2>/dev/null || git remote set-url origin "$REPO_URL"
else
    # Nếu đã có remote origin, kiểm tra URL
    EXISTING_ORIGIN=$(git remote get-url origin 2>/dev/null || true)
    if [ -z "$EXISTING_ORIGIN" ]; then
        git remote add origin "$REPO_URL"
    fi
fi

# Lấy branch hiện tại nếu đang ở trong git repo
if git rev-parse --abbrev-ref HEAD >/dev/null 2>&1; then
    CURRENT_BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "main")
    if [ "$1" == "" ] && [ "$CURRENT_BRANCH" != "HEAD" ] && [ -n "$CURRENT_BRANCH" ]; then
        BRANCH="$CURRENT_BRANCH"
    fi
fi

echo -e "  Đang đồng bộ với nhánh: ${CYAN}$BRANCH${NC} từ ${BLUE}$REPO_URL${NC}..."

# Kéo code mới từ origin
git fetch origin "$BRANCH" --depth=1 || git fetch origin "$BRANCH"
git checkout -B "$BRANCH" "origin/$BRANCH"

# Khôi phục file cấu hình đã sao lưu để không bị đè bởi code mới
if [ -f "$BACKUP_DIR/config_${TIMESTAMP}.json" ]; then
    cp "$BACKUP_DIR/config_${TIMESTAMP}.json" "config.json"
    echo -e "  ${GREEN}✓${NC} Đã khôi phục cài đặt config.json của bạn"
fi
if [ -f "$BACKUP_DIR/env_${TIMESTAMP}.bak" ]; then
    cp "$BACKUP_DIR/env_${TIMESTAMP}.bak" ".env"
    echo -e "  ${GREEN}✓${NC} Đã khôi phục cài đặt .env của bạn"
fi

LATEST_COMMIT=$(git log -1 --format="%h - %s (%cr)" 2>/dev/null || echo "Updated")
echo -e "  ${GREEN}✓${NC} Mã nguồn mới nhất: ${YELLOW}$LATEST_COMMIT${NC}"

# ------------------------------------------------------------------------------
# 3. Kiểm tra WebUI
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[3/5] Kiểm tra giao diện WebUI...${NC}"
if [ -f "static/admin/index.html" ]; then
    echo -e "  ${GREEN}✓${NC} Giao diện WebUI đã có sẵn trong static/admin/."
elif command -v npm >/dev/null 2>&1 && [ -d "webui" ]; then
    echo -e "  Đang biên dịch lại WebUI..."
    cd "$PROJECT_DIR/webui"
    npm ci --prefer-offline --no-audit 2>/dev/null || npm install --prefer-offline 2>/dev/null
    npm run build
    cd "$PROJECT_DIR"
    echo -e "  ${GREEN}✓${NC} WebUI đã được biên dịch thành công!"
else
    echo -e "  ${YELLOW}!${NC} Cảnh báo: Chưa có static/admin/index.html. Đang thử tải từ bản build..."
fi

# ------------------------------------------------------------------------------
# 4. Biên dịch Go Binary (ds2api)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[4/5] Biên dịch ứng dụng Go (ds2api)...${NC}"

if [ ! -f "go.mod" ]; then
    echo -e "${RED}✗ LỖI: Không tìm thấy tệp go.mod! Vui lòng kiểm tra lại quyền truy cập vào Git repository.${NC}"
    exit 1
fi

if command -v go >/dev/null 2>&1; then
    echo -e "  Phiên bản Go: $(go version)"
    echo -e "  Đang biên dịch binary tĩnh (CGO_ENABLED=0)..."
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ds2api ./cmd/ds2api
    chmod +x ds2api
    echo -e "  ${GREEN}✓${NC} Biên dịch ds2api thành công! ($(ls -lh ds2api | awk '{print $5}'))"
else
    echo -e "${RED}✗ LỖI: Không tìm thấy trình biên dịch Go ('go') trên VPS!${NC}"
    echo -e "  Vui lòng cài đặt Go bằng lệnh: ${CYAN}apt-get update && apt-get install -y golang${NC}"
    exit 1
fi

# ------------------------------------------------------------------------------
# 5. Khởi động lại dịch vụ (aaPanel Supervisor, Systemd, PM2, hoặc Nohup)
# ------------------------------------------------------------------------------
echo ""
echo -e "${BLUE}[5/5] Khởi động lại dịch vụ...${NC}"

RESTARTED=false

# Cách 1: aaPanel Supervisor Manager
if command -v supervisorctl >/dev/null 2>&1; then
    if supervisorctl status ds2api 2>/dev/null | grep -qE "RUNNING|STOPPED|FATAL"; then
        echo -e "  Phát hiện Supervisor aaPanel [ds2api], đang khởi động lại..."
        supervisorctl restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} Supervisor: Đã khởi động lại ds2api!"
    fi
fi

# Cách 2: Systemd service
if [ "$RESTARTED" = false ] && command -v systemctl >/dev/null 2>&1; then
    if systemctl list-unit-files 2>/dev/null | grep -q "ds2api.service"; then
        echo -e "  Phát hiện Systemd service [ds2api], đang khởi động lại..."
        systemctl restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} Systemd: Đã khởi động lại ds2api!"
    fi
fi

# Cách 3: PM2 Process Manager
if [ "$RESTARTED" = false ] && command -v pm2 >/dev/null 2>&1; then
    if pm2 list 2>/dev/null | grep -q "ds2api"; then
        echo -e "  Phát hiện PM2 [ds2api], đang khởi động lại..."
        pm2 restart ds2api
        RESTARTED=true
        echo -e "  ${GREEN}✓${NC} PM2: Đã khởi động lại ds2api!"
    fi
fi

# Cách 4: Quản lý tiến trình trực tiếp
if [ "$RESTARTED" = false ]; then
    echo -e "  Không phát hiện Supervisor/Systemd, đang khởi động lại tiến trình nền..."
    pkill -f "./ds2api" || true
    sleep 1
    nohup ./ds2api > ds2api.log 2>&1 &
    NEW_PID=$!
    echo -e "  ${GREEN}✓${NC} Đã chạy ds2api ở chế độ nền (PID: ${CYAN}$NEW_PID${NC})"
    echo -e "  Nhật ký hoạt động: ${BLUE}$PROJECT_DIR/ds2api.log${NC}"
    RESTARTED=true
fi

# ------------------------------------------------------------------------------
# Kiểm tra sức khỏe dịch vụ (Health Check)
# ------------------------------------------------------------------------------
echo ""
echo -e "${CYAN}Đang kiểm tra trạng thái hoạt động...${NC}"
sleep 2

PORT=5001
if [ -f "config.json" ]; then
    CFG_PORT=$(grep -o '"port"[[:space:]]*:[[:space:]]*[0-9]*' config.json | grep -o '[0-9]*' || true)
    if [ -n "$CFG_PORT" ]; then
        PORT=$CFG_PORT
    fi
fi

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
    echo -e "${YELLOW}  Đã cập nhật xong mã nguồn. Hãy kiểm tra trạng thái dịch vụ!  ${NC}"
    echo -e "${YELLOW}==============================================================${NC}"
    echo -e "  Xem log: ${CYAN}tail -n 50 ds2api.log${NC}"
fi

echo ""

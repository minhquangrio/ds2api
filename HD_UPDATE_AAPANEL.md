# Hướng Dẫn Cập Nhật Mã Nguồn Trực Tiếp Trên VPS aaPanel

File script [`update.sh`](file:///d:/Ds2apideepseek/update.sh) được tạo ra nhằm giúp bạn cập nhật mã nguồn **DS2API** trực tiếp trên VPS aaPanel chỉ với **1 lệnh duy nhất**.

Script tự động thực hiện trọn gói:
- 🛡️ **Tự động sao lưu dữ liệu**: Luôn backup `config.json` và `.env` trước khi cập nhật (giữ lại 10 bản gần nhất trong thư mục `backups/`), đảm bảo **không bao giờ bị mất tài khoản hoặc cấu hình**.
- 🔄 **Kéo code mới nhất**: Tự động `git fetch` và cập nhật nhánh mới nhất.
- 🏗️ **Build WebUI**: Tự động build lại giao diện quản trị Admin WebUI mới nhất.
- ⚡ **Biên dịch Go Binary**: Tự động biên dịch lại binary `ds2api` tĩnh tối ưu cho Linux.
- 🚀 **Khởi động lại thông minh**: Tự động nhận diện và restart theo đúng công cụ bạn dùng trên aaPanel (aaPanel **Supervisor**, **Systemd**, **PM2**, hoặc **Nohup**).
- 🩺 **Kiểm tra sức khỏe**: Tự động gọi probe `/healthz` để xác nhận server đã online sau khi update.

---

## 1. Cách Cập Nhật Bằng Terminal aaPanel (Khuyên Dùng)

### Bước 1: Mở Terminal trên aaPanel
1. Đăng nhập vào trang quản trị aaPanel của bạn.
2. Nhấp vào mục **Terminal** ở thanh menu bên trái (hoặc SSH vào VPS bằng PuTTY / MobaXterm / VS Code).

### Bước 2: Chạy lệnh cập nhật
Chuyển vào thư mục cài đặt dự án (ví dụ: `/www/wwwroot/ds2api`) và chạy:

```bash
cd /www/wwwroot/ds2api
bash update.sh
```

> **Mẹo**: Nếu bạn muốn cập nhật một nhánh Git cụ thể (ví dụ nhánh `main` hoặc `dev`):
> ```bash
> bash update.sh main
> ```

---

## 2. Cách Cấu Hình Quản Lý Tiến Trình Trên aaPanel

Để `update.sh` tự động khởi động lại dịch vụ mượt mà, bạn nên dùng 1 trong 2 cách sau:

### Cách A: Dùng aaPanel Supervisor Manager (Khuyên dùng nhất trên aaPanel)
1. Trong aaPanel, vào **App Store** -> Tìm cài đặt **Supervisor Manager** (trình giám sát tiến trình của aaPanel).
2. Mở Supervisor Manager -> Bấm **Add Daemon** (Thêm tiến trình):
   - **Name**: `ds2api`
   - **Run User**: `root` hoặc `www`
   - **Run Dir**: `/www/wwwroot/ds2api` (thư mục dự án)
   - **Start Command**: `/www/wwwroot/ds2api/ds2api`
   - **Processes**: `1`
3. Lưu lại và bấm **Start**.
4. Khi chạy `bash update.sh`, script sẽ tự phát hiện Supervisor và restart `ds2api` ngay lập tức!

---

### Cách B: Dùng Systemd Service
Nếu bạn không cài Supervisor trên aaPanel, có thể tạo Systemd service:

1. Tạo file service:
   ```bash
   nano /etc/systemd/system/ds2api.service
   ```
2. Dán nội dung sau:
   ```ini
   [Unit]
   Description=DS2API Service
   After=network.target

   [Service]
   Type=simple
   User=root
   WorkingDirectory=/www/wwwroot/ds2api
   ExecStart=/www/wwwroot/ds2api/ds2api
   Restart=always
   RestartSec=5

   [Install]
   WantedBy=multi-user.target
   ```
3. Kích hoạt và khởi động:
   ```bash
   systemctl daemon-reload
   systemctl enable ds2api
   systemctl start ds2api
   ```

---

## 3. Cách Tự Động Hóa 100% Qua aaPanel Webhook (Tùy chọn)

Nếu bạn muốn: **Cứ push code lên GitHub là VPS aaPanel tự update**:

1. Trên aaPanel -> **App Store** -> Cài đặt **Webhook**.
2. Mở Webhook -> Thêm webhook mới:
   - **Name**: `Update DS2API`
   - **Code**:
     ```bash
     cd /www/wwwroot/ds2api
     bash update.sh >> /www/wwwroot/ds2api/update.log 2>&1
     ```
3. aaPanel sẽ cấp cho bạn một đường link dạng:
   `https://vps-ip:8888/hook?access_key=xxxx&params=...`
4. Vào repository GitHub -> **Settings** -> **Webhooks** -> **Add webhook** -> Dán link trên vào mục **Payload URL**.
5. Giờ đây, mỗi khi bạn đẩy code mới lên GitHub, aaPanel sẽ tự động kéo code, build và khởi động lại phiên bản mới nhất mà bạn không cần chạm tay vào VPS!

---

## 4. Xử Lý Các Lỗi Thường Gặp

### Lỗi thiếu quyền thực thi `update.sh`
```bash
chmod +x update.sh
```

### Lỗi VPS chưa cài Golang
Nếu VPS của bạn chưa có Go, cài nhanh bằng lệnh:
- **Ubuntu / Debian**:
  ```bash
  apt-get update && apt-get install -y golang
  ```
- **CentOS / AlmaLinux**:
  ```bash
  yum install -y golang
  ```

### Lỗi VPS chưa cài Node.js / NPM (để build WebUI)
- Trên aaPanel, vào **App Store** -> Cài đặt **Node.js Version Manager** -> Chọn cài đặt phiên bản Node LTS (v20 hoặc v22).

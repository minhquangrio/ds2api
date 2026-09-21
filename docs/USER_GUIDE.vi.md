# Hướng Dẫn Sử Dụng Chi Tiết Phần Mềm DS2API

Tài liệu này hướng dẫn chi tiết từ A đến Z cách thiết lập, cấu hình, quản lý WebUI và kết nối phần mềm **DS2API** với các ứng dụng phổ biến (như NextChat, ChatBox, Claude Code, Cursor, One API, v.v.).

---

## 1. Giới Thiệu Tổng Quan

**DS2API** là một hệ thống Cổng chuyển đổi (Gateway) hiệu năng cao viết bằng ngôn ngữ Go. Nhiệm vụ chính của ứng dụng là chuyển đổi giao diện trò chuyện Web của DeepSeek thành API chuẩn tương thích hoàn toàn với:
- **OpenAI API** (`/v1/chat/completions`, `/v1/responses`, `/v1/models`, v.v.)
- **Claude / Anthropic API** (`/anthropic/v1/messages`, `/v1/messages`)
- **Google Gemini API** (`/v1beta/models/*:generateContent`)
- **Ollama API** (`/api/tags`, `/api/show`)

Ứng dụng đi kèm bảng quản trị trực quan **WebUI** hỗ trợ đa ngôn ngữ (Tiếng Việt, Tiếng Anh, Tiếng Trung) giúp người dùng quản lý danh sách tài khoản, khoá truy cập API, nút proxy và theo dõi lịch sử hệ thống một cách dễ dàng.

---

## 2. Chuẩn Bị Cấu Hình Ban Đầu

Trước khi khởi chạy, bạn cần tạo tập tin cấu hình `config.json` từ tập tin mẫu `config.example.json`.

### Các bước khởi tạo `config.json`:
1. Mở thư mục dự án và sao chép tập tin mẫu:
   ```bash
   cp config.example.json config.json
   ```
2. Mở `config.json` bằng trình chỉnh sửa văn bản (VS Code, Notepad, Nano, v.v.) và điền các thông tin:

#### Cấu trúc cấu hình cơ bản:
```json
{
  "keys": [
    "sk-ds2api-custom-key-1",
    "sk-ds2api-custom-key-2"
  ],
  "accounts": [
    {
      "email": "tai_khoan_1@gmail.com",
      "password": "mat_khau_tai_khoan_1",
      "name": "Tài khoản chính A",
      "remark": "Dùng cá nhân",
      "tools_enabled": true
    },
    {
      "mobile": "+84912345678",
      "password": "mat_khau_tai_khoan_2",
      "name": "Tài khoản phụ B",
      "tools_enabled": true
    }
  ],
  "security": {
    "admin_key": "thay_doi_mat_khau_admin_vi_ly_do_bao_mat"
  }
}
```

### Giải thích các thuộc tính quan trọng:
- `keys` (hoặc `api_keys`): Danh sách khoá API mà bạn tự quy định để cấp cho các client (NextChat, Cursor, v.v.) kết nối vào DS2API.
- `accounts`: Danh sách tài khoản DeepSeek Web mà bạn sở hữu. Hỗ trợ đăng nhập qua `email` hoặc `mobile` (số điện thoại kèm mã quốc gia như `+84`).
- `security.admin_key`: Mật khẩu dùng để đăng nhập vào Bảng quản trị WebUI (`/admin`).

---

## 3. Hướng Dẫn Triển Khai & Khởi Động

Bạn có thể lựa chọn 1 trong 4 phương thức triển khai sau tùy theo nhu cầu:

### Phương thức 1: Chạy trực tiếp từ bản Release (Khuyên dùng cho máy cá nhân/VPS)
1. Tải bản nén phù hợp với hệ điều hành của bạn từ trang **GitHub Releases**.
2. Giải nén tập tin archive (`tar -xzf` trên Linux/macOS hoặc dùng WinRAR/7-Zip trên Windows).
3. Tạo tập tin `config.json` trong cùng thư mục với tập tin thực thi `ds2api`.
4. Khởi chạy ứng dụng:
   - **Windows**: Nhấp kép vào `ds2api.exe` hoặc chạy `./ds2api.exe` từ PowerShell.
   - **Linux / macOS**: Chạy `./ds2api` trong Terminal.
5. Truy cập Bảng quản trị WebUI tại: `http://localhost:5001/admin`

### Phương thức 2: Chạy bằng Docker / Docker Compose (Khuyên dùng cho Server)
1. Tải tập tin `docker-compose.tar.gz` từ Release và giải nén:
   ```bash
   mkdir ds2api && cd ds2api
   wget https://github.com/ouqiting/ds2api/releases/latest/download/docker-compose.tar.gz
   tar -xzf docker-compose.tar.gz
   ```
2. Tạo tệp `.env` và `config.json`:
   ```bash
   cp .env.example .env
   cp config.example.json config.json
   ```
3. Chỉnh sửa `.env` đặt `DS2API_ADMIN_KEY=MatKhauBaoMatCuaBan`.
4. Khởi chạy container:
   ```bash
   docker compose up -d
   ```

### Phương thức 3: Triển khai Miễn Phí / Serverless (Zeabur / Vercel)
- **Zeabur**: Nhấp vào nút **Deploy on Zeabur** trên trang chủ kho lưu trữ, đặt cấu hình biến môi trường `DS2API_ADMIN_KEY` và gắn volume lưu trữ `/data`.
- **Vercel**: Fork dự án về GitHub cá nhân, import vào Vercel, mã hóa `config.json` thành chuỗi Base64 (`base64 < config.json | tr -d '\n'`) và dán vào biến môi trường `DS2API_CONFIG_JSON`.

---

## 4. Hướng Dẫn Sử Dụng Bảng Quản Trị WebUI (`/admin`)

Mở trình duyệt và truy cập `http://localhost:5001/admin`.

### 4.1 Đăng nhập
- Nhập **Khoá admin** (`DS2API_ADMIN_KEY` trong `config.json` hoặc biến môi trường).
- Tích chọn "Ghi nhớ phiên này" để không cần đăng nhập lại lần sau.

### 4.2 Chuyển đổi Ngôn ngữ & Giao diện (Theme)
- Ở góc trên bên phải thanh điều hướng, nhấp vào icon quả địa cầu (`中` / `EN` / `VI`) để chọn **VI · Tiếng Việt**.
- Nhấp icon Mặt trời/Mặt trăng để chuyển đổi giữa **Giao diện tối (Dark Mode)** và **Giao diện sáng (Light Mode)**.

### 4.3 Dashboard Tổng quan Token (Token Overview)
- **Chỉ số lưu lượng Token**: Theo dõi tổng số token đã sử dụng, phân rã chi tiết giữa *Prompt Tokens* (đầu vào), *Completion Tokens* (đầu ra) và *Reasoning Tokens* (suy luận R1).
- **Hiệu năng & Độ tin cậy**: Hiển thị tỷ lệ thành công của request (% 200 OK) và độ trễ phản hồi trung bình (`ms`).
- **Biểu đồ tiêu thụ**: Biểu đồ trực quan hoá lưu lượng token theo thời gian thực.
- **Tỷ lệ phân bổ Model**: Thống kê mức độ tiêu thụ của từng model (`deepseek-chat`, `deepseek-reasoner`, `gemini-2.5`, v.v.).
- **Tích hợp nhanh (Quick Integration)**: Xem nhanh Base URL (`http://.../v1`), khoá API mặc định và sao chép mã mẫu gọi API (cURL, Python `OpenAI`, Node.js `OpenAI`).

### 4.4 Quản lý Khoá API (API Keys Console)
- Phân hệ độc lập quản lý toàn bộ khoá API cấp cho các ứng dụng client.
- **Tạo khoá mới**: Tạo khoá thủ công hoặc nhấp **Tạo ngẫu nhiên** để sinh khoá an toàn có tiền tố `sk-ds2-...`.
- **Ẩn/Hiện & Sao chép**: Nhấp icon con mắt để ẩn/hiện chuỗi khoá, nhấp icon Copy để sao chép vào bộ nhớ tạm.
- **Chỉnh sửa & Xoá**: Đổi tên gợi nhớ, ghi chú và bật/tắt quyền Tool Calls cho từng key.

### 4.5 Quản lý Cụm Tài khoản Upstream (DeepSeek & Gemini)
- **Thêm tài khoản mới**: Nhấp nút **Thêm tài khoản**, nhập Email/SĐT, Mật khẩu hoặc Cookies/Token, Tên gợi nhớ và Ghi chú.
- **Làm mới & Kiểm tra**: Nhấp nút **Kiểm tra tất cả** để kiểm tra trạng thái đăng nhập của toàn bộ tài khoản trong pool. Đối với tài khoản Gemini Web, thao tác kiểm tra sẽ chủ động liên hệ Google RotateCookies để xác thực và làm mới chuỗi cookie `__Secure-1PSIDTS` trực tuyến (có cooldown bảo vệ tối thiểu 10s giữa các lần bấm để tránh bị Google giới hạn rate limit 429).
- **Tự động làm mới Cookie Gemini**: Máy chủ chạy nền (`DS2API_GEMINI_COOKIE_REFRESH_INTERVAL`, mặc định 10 phút) sẽ tự động xoay và cập nhật cookie mới vào `config.json` cho các tài khoản Gemini đang hoạt động, giúp phiên làm việc luôn được duy trì liên tục mà không cần cập nhật thủ công.
- **Bật / Tắt tài khoản**: Bạn có thể bật/tắt thủ công từng tài khoản hoặc dùng nút hàng loạt để tạm ngưng tài khoản khi cần.
- **Elastic Pool (Pool linh hoạt)**: Khi bật tính năng này, hệ thống sẽ tự động kích hoạt số lượng tài khoản chỉ định và tự động thay thế bằng tài khoản mới trong pool khi có tài khoản bị giới hạn hoặc lỗi.
- **Giám sát Hạn mức Gemini (Gemini Compute Quota Pool)**:
  - Tự động phát hiện khi có tài khoản Gemini trong danh sách và hiển thị card quản trị hạn mức chuyên biệt.
  - **Trường hợp 1 tài khoản**: Hiển thị hạn mức 5 giờ trượt (Compute Credits), tỷ lệ tiêu thụ %, hạn mức tuần, AI Credits và hạn mức của từng model cụ thể kèm thời gian tự động reset.
  - **Trường hợp nhiều tài khoản (Pool)**: Tự động tổng hợp tổng năng lực tính toán toàn cụm (Total Available 5h Credits), số tài khoản Sẵn sàng (Ready) so với Tạm nghẽn (Throttled / 100%), và thời điểm hồi phục sớm nhất của node bị nghẽn (`Next Reset`).
  - Hỗ trợ nút **Làm mới tất cả hạn mức** (đồng bộ song song có kiểm soát luồng) và nút xem chi tiết từng tài khoản.

### 4.6 Quản lý Proxy IP
- Nếu các tài khoản DeepSeek của bạn cần chạy qua các địa chỉ IP đầu ra khác nhau để tránh bị trùng IP, bạn có thể thêm các nút **HTTP**, **HTTPS**, **SOCKS5** hoặc **SOCKS5H** trong tab **Proxy IP**.
- Nhấp **Kiểm tra proxy** để đo độ trễ kết nối từ máy chủ tới máy chủ DeepSeek.

### 4.7 Nhật ký & Chi tiết Token (Logs & Token Ledger)
- Xem chi tiết từng lượt gọi API qua Gateway kèm mã trạng thái HTTP, thời gian xử lý (`elapsed_ms`), model gọi và số lượng prompt/completion tokens chính xác.
- Nhấp vào từng dòng để xem payload hội thoại và phản hồi hoàn chỉnh.

### 4.8 Trình Kiểm Tra API (API Test)
- Cung cấp giao diện playground nhắn tin thử nghiệm trực tiếp ngay trong WebUI.
- Hỗ trợ chọn mô hình, bật/tắt chế độ Streaming, và chọn tài khoản chỉ định hoặc xoay vòng ngẫu nhiên.

---

## 5. Hướng Dẫn Kết Nối Với Các Ứng Dụng Phổ Biến

### 5.1 Kết nối với NextChat / ChatBox / LobeChat
1. Mở phần Cài đặt (Settings) trong ứng dụng client.
2. Chọn nhà cung cấp: **OpenAI**.
3. Điền **API Key**: Nhập khoá API bạn đã tạo trong `config.json` (ví dụ: `sk-ds2api-custom-key-1`).
4. Điền **API Endpoint / Base URL**:
   - `http://localhost:5001/v1` (hoặc domain VPS của bạn).
5. Chọn mô hình: `deepseek-v4-flash` hoặc `deepseek-v4-pro`.

### 5.2 Kết nối với Claude Code (CLI)
1. Trong terminal của bạn, thiết lập 2 biến môi trường:
   ```bash
   # Trên Linux / macOS
   export ANTHROPIC_BASE_URL="http://localhost:5001"
   export ANTHROPIC_API_KEY="sk-ds2api-custom-key-1"

   # Trên Windows PowerShell
   $env:ANTHROPIC_BASE_URL="http://localhost:5001"
   $env:ANTHROPIC_API_KEY="sk-ds2api-custom-key-1"
   ```
2. Khởi chạy `claude`. Claude Code sẽ kết nối với DS2API qua endpoint `/v1/messages`.

### 5.3 Kết nối với Cursor / VS Code Extensions
1. Mở Cài đặt Cursor -> **Models** -> **OpenAI API Key**.
2. Nhập API Key: `sk-ds2api-custom-key-1`.
3. Bật tùy chọn **Override OpenAI Base URL** và nhập:
   `http://localhost:5001/v1`
4. Thêm tên mô hình tùy chỉnh: `deepseek-v4-flash` hoặc `deepseek-v4-pro`.

### 5.4 Tích hợp vào One API / New API (Quản lý Kênh API)
1. Vào mục **Kênh (Channels)** -> **Thêm kênh mới**.
2. Loại kênh: **OpenAI** (hoặc **Anthropic** / **Gemini**).
3. Tên kênh: `DS2API Local`.
4. Giá trị Base URL: `http://localhost:5001` (hoặc `http://localhost:5001/v1`).
5. Khoá API: Nhập khoá trong `config.json`.

---

## 6. Danh Sách Mô Hình Hỗ Trợ

| ID Mô hình | Chế độ Suy luận (Thinking) | Tìm kiếm Web (Search) | Ghi chú |
| --- | --- | --- | --- |
| `deepseek-v4-flash` | Bật mặc định | ❌ | Tốc độ nhanh, phản hồi tức thì |
| `deepseek-v4-flash-nothinking` | Tắt vĩnh viễn | ❌ | Phản hồi siêu tốc, tắt suy luận sâu |
| `deepseek-v4-pro` | Bật mặc định | ❌ | Mô hình chuyên gia (Expert/R1), suy luận chuyên sâu |
| `deepseek-v4-pro-nothinking` | Tắt vĩnh viễn | ❌ | Mô hình chuyên gia không suy luận |
| `deepseek-v4-flash-search` | Bật mặc định | ✅ | Tích hợp tìm kiếm Web thời gian thực |
| `deepseek-v4-flash-search-nothinking` | Tắt vĩnh viễn | ✅ | Tìm kiếm Web không suy luận |
| `deepseek-v4-vision` | Bật mặc định | ❌ | Hỗ trợ phân tích hình ảnh (Multimodal) |
| `deepseek-v4-vision-nothinking` | Tắt vĩnh viễn | ❌ | Phân tích hình ảnh không suy luận |

Ngoài ra, ứng dụng cũng hỗ trợ tự động ánh xạ các tên bí danh thông dụng như `gpt-4o`, `gpt-4`, `claude-sonnet-4-6`, `gemini-2.5-pro` về mô hình DeepSeek tương ứng.

---

## 7. Xử Lý Sự Cố Thường Gặp (Troubleshooting)

### 7.1 Lỗi `429 Too Many Requests`
- **Nguyên nhân**: Số lượng yêu cầu đồng thời vượt quá giới hạn hàng chờ của số tài khoản DeepSeek hiện có.
- **Khắc phục**:
  1. Thêm thêm tài khoản DeepSeek vào pool trong Bảng quản trị WebUI.
  2. Nâng tham số `account_max_inflight` (ví dụ từ `2` lên `3` hoặc `4`) và `account_max_queue` trong mục **Settings**.

### 7.2 Lỗi Tài khoản bị tạm khóa / Yêu cầu xác minh
- **Nguyên nhân**: Hệ thống phía trên của DeepSeek yêu cầu làm mới xác thực token.
- **Khắc phục**: Vào WebUI -> mục **Tài khoản** -> Nhấp nút **Làm mới tất cả token**. Nếu tài khoản bị đổi mật khẩu hoặc lỗi thông tin đăng nhập, hãy nhấp vào biểu tượng chiếc bút để cập nhật lại mật khẩu mới.

### 7.3 Lỗi kết nối CORS từ Web Client
- **Khắc phục**: DS2API đã tích hợp bộ định tuyến CORS tự động cho phép mọi Nguồn (Origin). Kiểm tra xem firewall hoặc proxy trung gian (Nginx/Cloudflare) của bạn có chặn các header tùy chỉnh hay không.

### 7.4 Cách bật tính năng Ghi gói tin Debug (Packet Capture)
Khi cần gỡ lỗi luồng suy luận hoặc gọi công cụ (Tool Call), bạn có thể chạy DS2API với biến môi trường bắt gói tin:
```bash
DS2API_DEV_PACKET_CAPTURE=true go run ./cmd/ds2api
```
Sau đó truy cập Bảng quản trị WebUI để xem chi tiết gói tin request/response giữa DS2API và máy chủ DeepSeek.

---

## 8. Mục Luc Tài Liệu Tham Khảo

- [Tài liệu Kiến trúc hệ thống](ARCHITECTURE.md)
- [Tài liệu Giao diện API](API.md) / [Tài liệu API tiếng Anh](../API.en.md)
- [Hướng dẫn Triển khai chi tiết](DEPLOY.md)
- [Hướng dẫn Đóng góp mã nguồn](CONTRIBUTING.md)

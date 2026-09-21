# Kế Hoạch Triển Khai Hiển Thị Hạn Mức & Lượt Dùng Gemini (Tương Tự Gemini Proxy)

Kế hoạch này mô tả giải pháp tích hợp đầy đủ tính năng kiểm tra và hiển thị hạn mức (Quota / Compute Usage Limits) cho các tài khoản Gemini trong **DS2API** (`d:\Ds2apideepseek`), mô phỏng và nâng cấp từ tính năng có sẵn trong **Gemini Proxy** (`D:\Software\Gemini Proxy`).

---

## 1. Khảo Sát Tính Năng Trong Gemini Proxy

Trong `D:\Software\Gemini Proxy`, tính năng theo dõi hạn mức được tổ chức qua các thành phần:

1. **RPC `jSf9Qc` (`GET_USAGE_INFO`)**:
   - Gửi yêu cầu batchexecute với `source-path=/usage`, payload `[]`.
   - Google trả về cấu trúc tính toán điện toán (compute-based usage):
     - **Cấp bậc gói tài khoản (`tier`)**: ID `1` (FREE), `2` (PRO), `3` (ULTRA), `4` (PLUS), `6` (ULTRA).
     - **Cửa sổ 5 giờ (`current_5h`)**: Loại `1`, số lượt còn lại (`remaining_credits`), phần trăm sử dụng (`usage_percentage`), thời gian hoàn trả hạn mức (`reset_at`).
     - **Cửa sổ tuần (`weekly`)**: Loại `2`, số lượt tuần còn lại, % đã dùng, thời gian reset.
     - **AI Credits bổ sung (`ai_credits_remaining`)**: Nếu tài khoản có mua thêm credits.
2. **RPC `qpEbW` (`CHECK_GEMINI_QUOTA`)**:
   - Truy vấn hạn mức chi tiết cho từng mô hình:
     - Action `4`: **Gemini Pro** (remaining / total, reset_time, usage_percentage).
     - Action `11`: **Gemini Flash** (remaining / total, reset_time, usage_percentage).
     - Action `15`: **Gemini Flash Thinking** (remaining / total, reset_time, usage_percentage).
3. **RPC `aPya6c` (`CHECK_QUOTA`)**:
   - Kiểm tra xem các tính năng phụ trợ (code execution, search grounding, v.v.) có bị chặn (`is_blocked`) hoặc sử dụng bao nhiêu phần trăm hay không.
4. **Giao diện người dùng (UI Widget)**:
   - Hiển thị Huy hiệu gói tài khoản: `PRO`, `FREE`, `ULTRA`.
   - Khung "Hạn mức & Lượt dùng" với thanh tiến trình gradient (Quota Bar) phản ánh tỉ lệ còn lại.
   - Hiển thị thời gian reset: `Reset lúc HH:MM`.
   - Dòng thống kê: `X lượt / 5 giờ còn lại (Y% đã dùng)`.

---

## 2. User Review Required

> [!IMPORTANT]
> **Điểm hiển thị trên WebUI**:
> 1. **Nút "Hạn mức" (Quota) trên bảng tài khoản**: Trong danh sách tài khoản (`AccountsTable.jsx`), mỗi tài khoản Gemini sẽ có thêm nút kiểm tra hạn mức (icon `Gauge`). Khi bấm vào sẽ mở **Gemini Quota Modal** hiển thị trực quan toàn bộ thông số (5h window, weekly window, quota từng model, trạng thái extra features).
> 2. **Tùy chọn hiển thị Quota Widget trong API Tester**: Khi chọn mô hình Gemini trong tab kiểm tra API (`ApiTesterContainer.jsx`), sẽ hiển thị Quota Widget nhỏ gọn để theo dõi số lượt dùng tiêu hao theo thời gian thực.
> 
> Hãy xác nhận nếu bạn muốn thêm hoặc điều chỉnh vị trí hiển thị nào khác.

---

## 3. Open Questions

> [!NOTE]
> Không có câu hỏi ngăn trở việc lập kế hoạch. Tất cả các cơ chế RPC (`jSf9Qc`, `qpEbW`, `aPya6c`) đã được phân tích từ `D:\Software\Gemini Proxy` và cấu trúc `httpcloak` của `Ds2apideepseek` đã sẵn sàng để tích hợp.

---

## 4. Proposed Changes

### Component 1: Gemini Web Engine (`internal/geminiweb`)

Mở rộng gói `internal/geminiweb` để hỗ trợ đầy đủ các RPC đo lường hạn mức và điện toán.

#### [MODIFY] [constants.go](file:///d:/Ds2apideepseek/internal/geminiweb/constants.go)
- Thêm hằng số RPC:
  - `RPCGetUsageInfo = "jSf9Qc"`
  - `RPCCheckExtraQuota = "aPya6c"`
- Thêm định nghĩa hằng số cho Tier ID:
  - `TierFree = 1`, `TierPro = 2`, `TierUltra = 3`, `TierPlus = 4`.

#### [MODIFY] [quota.go](file:///d:/Ds2apideepseek/internal/geminiweb/quota.go)
- Định nghĩa các struct dữ liệu chuẩn hóa:
  - `UsageMetric`: `{ Window, RemainingCredits, UsageLevel, UsagePercentage, ResetAt }`
  - `TierInfo`: `{ ID, Label }`
  - `UsageInfo`: `{ Tier, Current5h, Weekly, AICreditsRemaining }`
  - `ExtraQuotaInfo`: `{ IsBlocked, UsagePercentage, ResetTime }`
  - `GeminiAccountQuotaSummary`: Tổng hợp `UsageInfo`, `Quotas` (per-model), `ExtraQuota`, `FetchedAt`.
- Triển khai hàm `(c *Client) FetchUsageInfo(ctx context.Context) (*UsageInfo, error)`:
  - Gửi POST batchexecute với `rpcids=jSf9Qc`, `source-path=/usage`, payload `[]`.
  - Phân tích frame JSON trích xuất tier và các metrics (5h, weekly, credits).
- Triển khai hàm `(c *Client) FetchExtraQuota(ctx context.Context) (*ExtraQuotaInfo, error)`:
  - Gửi POST batchexecute với `rpcids=aPya6c`.
- Triển khai hàm `(c *Client) GetFullQuota(ctx context.Context, forceRefresh bool) (*GeminiAccountQuotaSummary, error)`:
  - Lấy cả compute usage (`FetchUsageInfo`), model quotas (`CheckQuota`), và extra features (`FetchExtraQuota`).
  - Tích hợp bộ đệm nhớ tạm (cache TTL 30-60 giây) trên `Client` để tránh spam request lên Google nếu người dùng bấm refresh liên tục.

#### [MODIFY] [quota_test.go](file:///d:/Ds2apideepseek/internal/geminiweb/quota_test.go)
- Viết unit test cho parser `ParseUsageInfoResponse` với dữ liệu mẫu chứa tier `PRO`, cửa sổ `5h` còn 45 credits (10% used), và `weekly`.
- Viết unit test cho parser `ParseExtraQuotaResponse`.

---

### Component 2: Backend Admin HTTP API (`internal/httpapi/admin/accounts`)

Cung cấp REST endpoint để giao diện quản trị truy vấn hạn mức tài khoản.

#### [NEW] [handler_accounts_quota.go](file:///d:/Ds2apideepseek/internal/httpapi/admin/accounts/handler_accounts_quota.go)
- Triển khai hàm `getAccountQuota(w http.ResponseWriter, r *http.Request)`:
  - Lấy `identifier` từ URL param qua `chi.URLParam(r, "identifier")`.
  - Tìm kiếm tài khoản trong `Store`. Nếu không tìm thấy, trả về 404.
  - Kiểm tra xem tài khoản có phải provider `gemini` không (`acc.IsGemini()`). Nếu không, trả về 400 (chỉ hỗ trợ tài khoản Gemini).
  - Lấy instance `geminiweb.Client` từ `geminiweb.DefaultRuntime().GetClient(r.Context(), acc, h.Store)`.
  - Kiểm tra query param `?refresh=true` để quyết định force refresh.
  - Gọi `client.GetFullQuota(r.Context(), forceRefresh)`.
  - Trả về JSON chứa thông tin hạn mức đầy đủ:
    ```json
    {
      "identifier": "gemini-pro-1",
      "provider": "gemini",
      "tier": { "id": 2, "label": "PRO" },
      "usage": {
        "current_5h": {
          "window": "5h",
          "remaining_credits": 45,
          "usage_percentage": 10,
          "reset_at": "2026-09-21T18:00:00Z"
        },
        "weekly": { ... },
        "ai_credits_remaining": null
      },
      "quotas": {
        "4": { "label": "Gemini Pro", "remaining": 45, "total": 50, "usage_percentage": 0.1, "reset_time": 1741234567 },
        "11": { "label": "Gemini Flash", "remaining": 100, "total": 100, "usage_percentage": 0.0, "reset_time": 1741234567 }
      },
      "extra_features": {
        "is_blocked": false,
        "usage_percentage": 0
      },
      "fetched_at": "2026-09-21T20:30:00Z"
    }
    ```

#### [MODIFY] [routes.go](file:///d:/Ds2apideepseek/internal/httpapi/admin/accounts/routes.go)
- Đăng ký route:
  ```go
  r.Get("/accounts/{identifier}/quota", h.getAccountQuota)
  ```

#### [MODIFY] [internal/server/router_routes_test.go](file:///d:/Ds2apideepseek/internal/server/router_routes_test.go)
- Thêm route `"GET /admin/accounts/{identifier}/quota"` vào danh sách test router.

#### [NEW] [handler_accounts_quota_test.go](file:///d:/Ds2apideepseek/internal/httpapi/admin/accounts/handler_accounts_quota_test.go)
- Unit test cho endpoint quota: kiểm tra tài khoản không tồn tại, tài khoản không phải gemini, và tài khoản gemini hợp lệ.

---

### Component 3: Frontend WebUI (`webui/src`)

Thiết kế giao diện đẹp mắt, tinh tế theo phong cách dark/light hiện đại.

#### [NEW] [GeminiQuotaModal.jsx](file:///d:/Ds2apideepseek/webui/src/features/account/GeminiQuotaModal.jsx)
- Tạo component Modal chi tiết hạn mức:
  - **Header**: Icon thước đo `Gauge`, Tên tài khoản, Identifier, Huy hiệu Tier (`PRO` màu tím gradient, `FREE` màu xanh xám, `ULTRA` màu vàng ánh kim), Nút Làm mới có spinner xoay.
  - **Widget 5 Giờ (Tâm điểm, tương đồng Gemini Proxy)**:
    - Tiêu đề "Hạn mức 5 giờ" & Reset countdown / `Reset lúc HH:MM`.
    - Thanh tiến trình mượt mà (Quota Bar Fill) phản ánh tỉ lệ còn lại (100 - % dùng), tự đổi màu:
      - Xanh ngọc / Emerald khi sử dụng < 75%.
      - Cam hổ phách khi sử dụng >= 75%.
      - Đỏ / Hồng ngọc khi sử dụng >= 90%.
    - Thống kê: `{remaining} lượt còn lại ({usage_percentage}% đã dùng)`.
  - **Widget Hạn Mức Tuần (Weekly Limit)** nếu có.
  - **Hạn Mức Theo Mô Hình (Per-Model Quotas)**:
    - Bảng hoặc các thẻ con cho `Gemini Pro`, `Gemini Flash`, `Gemini Flash Thinking` hiển thị: `{remaining}/{total}` lượt còn lại.
  - **Trạng Thái Tính Năng Mở Rộng**:
    - Hiển thị tính năng phụ trợ (Code Execution, Grounding Search) đang Khả dụng hay Bị khóa.
  - Nút đóng & Nút sao chép chẩn đoán JSON.

#### [MODIFY] [AccountsTable.jsx](file:///d:/Ds2apideepseek/webui/src/features/account/AccountsTable.jsx)
- Import icon `Gauge` từ `lucide-react`.
- Đối với các tài khoản `acc.provider === 'gemini'`:
  - Thêm nút hành động **"Hạn mức"** (`Gauge`) kế bên nút "Kiểm tra" (Test).
  - Khi nhấp, gọi callback `onViewQuota(acc)`.

#### [MODIFY] [AccountManagerContainer.jsx](file:///d:/Ds2apideepseek/webui/src/features/account/AccountManagerContainer.jsx) & [useAccountActions.js](file:///d:/Ds2apideepseek/webui/src/features/account/useAccountActions.js)
- Thêm state `quotaModalAccount` và hàm `handleViewQuota(acc)`, `handleFetchQuota(identifier, forceRefresh)`.
- Render `GeminiQuotaModal` khi người dùng mở xem hạn mức.

#### [MODIFY] [ApiTesterContainer.jsx](file:///d:/Ds2apideepseek/webui/src/features/apiTester/ApiTesterContainer.jsx)
- Khi mô hình được chọn là Gemini (`gemini-...`), hiển thị thanh Quota Widget thu nhỏ cạnh bộ chọn mô hình, cho phép xem nhanh số lượt 5h còn lại và bấm vào để xem chi tiết.

#### [MODIFY] [Locales (vi.json, en.json, zh.json)](file:///d:/Ds2apideepseek/webui/src/locales/vi.json)
- Bổ sung các bản dịch tiếng Việt, tiếng Anh và tiếng Trung cho các thuật ngữ:
  - `quotaModalTitle`: "Hạn Mức & Lượt Dùng Gemini"
  - `quota5hTitle`: "Hạn mức 5 giờ"
  - `quotaWeeklyTitle`: "Hạn mức tuần"
  - `quotaCreditsRemaining`: "{remaining} lượt còn lại ({used}% đã dùng)"
  - `quotaResetAt`: "Reset lúc {time}"
  - `quotaTier`: "Gói dịch vụ"
  - `quotaModelBreakdown`: "Hạn mức theo mô hình"
  - `quotaExtraFeatures`: "Tính năng bổ sung"
  - `quotaExtraBlocked`: "Bị tạm khóa"
  - `quotaExtraOk`: "Bình thường"
  - `quotaRefresh`: "Làm mới hạn mức"
  - `quotaActionBtn`: "Hạn mức"

---

### Component 4: Documentation Sync (`API.md`, `API.en.md`)

#### [MODIFY] [API.md](file:///d:/Ds2apideepseek/API.md) & [API.en.md](file:///d:/Ds2apideepseek/API.en.md)
- Bổ sung tài liệu API mô tả endpoint `GET /admin/accounts/{identifier}/quota`:
  - Quyền truy cập: Admin
  - Tham số: `identifier` (bắt buộc), `refresh` (query boolean, tùy chọn)
  - Mô tả kết quả trả về và mã lỗi.

---

## 5. Verification Plan

### Automated Tests
1. Chạy unit test của `internal/geminiweb`:
   ```powershell
   go test -v ./internal/geminiweb/...
   ```
2. Chạy unit test của `internal/httpapi/admin/accounts` và router test:
   ```powershell
   go test -v ./internal/httpapi/admin/accounts/...
   go test -v -run TestAllRoutesRegistered ./internal/server/...
   ```
3. Chạy toàn bộ unit test suite:
   ```powershell
   & "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/run-unit-all.sh"
   ```
4. Kiểm tra mã nguồn qua Go lint & refactor line gate:
   ```powershell
   & "C:\Program Files\Git\bin\bash.exe" -c "./scripts/lint.sh"
   & "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/check-refactor-line-gate.sh"
   ```
5. Kiểm tra build WebUI:
   ```powershell
   npm run build --prefix webui
   ```

### Manual Verification
1. Chạy backend cục bộ (`go run ./cmd/ds2api` hoặc file bat).
2. Mở WebUI trên trình duyệt, chuyển đến tab **Quản lý tài khoản**.
3. Tại dòng tài khoản Gemini đã cấu hình cookies hợp lệ, bấm vào nút **"Hạn mức"**.
4. Xác nhận modal hiển thị đầy đủ:
   - Huy hiệu gói (PRO / FREE / ULTRA).
   - Thanh tiến trình 5h với màu sắc trực quan và thời gian reset tương ứng múi giờ địa phương.
   - Thống kê lượt còn lại và phần trăm đã dùng.
   - Bảng phân bổ các mô hình Flash, Pro, Thinking.
5. Bấm nút **"Làm mới"** trong modal để xác nhận force-refresh hoạt động trơn tru.
6. Chuyển sang tab **Kiểm tra API**, chọn một mô hình Gemini, xác nhận Quota Widget hiển thị số lượt dùng còn lại.

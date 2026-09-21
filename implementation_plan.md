# Kế Hoạch Triển Khai Cơ Chế Tự Động Refresh Cookie Gemini (Bản Điều Chỉnh)

Tài liệu này là bản kế hoạch chi tiết đã được cập nhật sau khi tiếp thu toàn bộ các phản biện kỹ thuật, rà soát mã nguồn thực tế của cả `ds2api` và `Gemini Proxy`, đồng thời tuân thủ triệt để các quy tắc chất lượng trong [AGENTS.md](file:///d:/Ds2apideepseek/AGENTS.md).

---

## User Review Required

> [!IMPORTANT]
> **Đính chính so sánh với Gemini Proxy & Đánh giá rủi ro:**
> 1. **Parity thực tế:** Trong [Gemini Proxy](file:///d:/Software/Gemini%20Proxy), `rotate_1psidts` **chỉ được gọi duy nhất tại vòng lặp chạy nền định kỳ** ([client.py:471](file:///d:/Software/Gemini%20Proxy/src/gemini_webapi/client.py#L471)), hoàn toàn **không có cơ chế retry khi gặp 401**.
> 2. **Cơ chế Retry (Tính năng mới):** Việc retry khi gặp lỗi 401 trong `ds2api` là tính năng mở rộng. Để tránh rủi ro **thundering herd** khi cookie bị Google thu hồi (khiến hàng loạt request đồng thời cùng dính 401 và cùng gọi xoay cookie), hệ thống sẽ áp dụng:
>    - **Single-flight Mutex (`rotateMu`):** Chỉ một goroutine được phép gửi request xoay cookie tại một thời điểm.
>    - **Fail-Fast khi Cooldown:** Nếu vừa xoay cookie trong vòng 60 giây mà request vẫn dính 401, hệ thống sẽ trả lỗi `ErrUnauthenticated` ngay lập tức, **không xoay lại** và **không gọi lại InitSession với cookie cũ**.
> 3. **Hành vi trên Vercel:** Vercel là môi trường Serverless (stateless & read-only). Trên Vercel, background worker định kỳ sẽ **không được kích hoạt**, và cookie xoay trong bộ nhớ sẽ không tự động ghi vào `config.json` (vì Vercel bỏ qua ghi đĩa theo [store.go:287](file:///d:/Ds2apideepseek/internal/config/store.go#L287)).

> [!NOTE]
> **Tối ưu hóa tần suất ghi đĩa và lưu lượng mạng:**
> - **Skip Discovery:** Khi xoay cookie định kỳ, hệ thống sẽ bỏ qua bước `DiscoverModels` (chỉ lấy `AccessToken` và `BuildLabel`), tránh gây bão request thừa mỗi 10 phút.
> - **Batch Cookie Update:** Gom toàn bộ cookies trong header `Set-Cookie` và chỉ kích hoạt lưu vào `Store` một lần duy nhất nếu chuỗi cookie có thay đổi thực sự.
> - **Phạm vi Worker:** Chỉ refresh các client đang được nạp trong bộ nhớ của `Runtime` (các tài khoản đang active), kèm cơ chế ngắt tạm thời (backoff) nếu tài khoản thất bại 3 lần liên tiếp để chống spam log.

---

## Phân Tích Kiến Trúc & Sai Lệch Đã Khắc Phục

| Vấn đề phát hiện | Thiết kế sai ban đầu | Thiết kế chuẩn sau điều chỉnh |
| :--- | :--- | :--- |
| **Vị trí khởi động Worker** | Khởi động trong `NewApp()` | Chuyển sang [cmd/ds2api/main.go](file:///d:/Ds2apideepseek/cmd/ds2api/main.go) (chỉ chạy cho server dài hạn, bỏ qua trên Vercel và unit tests) |
| **Bảo vệ đồng thời** | Chỉ có mutex đọc `lastRotated` | Trang bị `rotateMu sync.Mutex` độc lập đảm bảo single-flight cho toàn bộ cụm Check-Rotate-Init |
| **Xử lý 401** | String matching | Sentinel error `var ErrUnauthenticated = errors.New(...)` kết hợp `errors.Is` |
| **Line Gate `runtime_stream.go`** | Dự kiến nhét retry vào 3 hàm stream (vi phạm gate 300 dòng) | Đặt retry tại hàm chung [prepareStream](file:///d:/Ds2apideepseek/internal/geminiweb/runtime_stream.go#L259) thông qua `StreamGenerateWithRetry`, không tăng dòng nào |
| **Protocol Adapter Boundary** | Từng adapter tự xử lý retry | Chuẩn hóa tầng retry trong package `geminiweb` (`StreamGenerateWithRetry`, `GenerateWithRetry`), protocol adapter chỉ gọi chuẩn |
| **Ghi đĩa liên tục** | Mỗi cookie gọi một lần `UpdateCookie` | Gom batch update trong `checkSetCookies`, chỉ cập nhật khi header thay đổi |
| **Khả năng kiểm thử (Test seam)** | URL dạng `const`, không test offline được | Bổ sung trường override URL / test seams trong `Client` để mock bằng `httptest.Server` |

---

## Proposed Changes

Toàn bộ file trong danh sách [plans/refactor-line-gate-targets.txt](file:///d:/Ds2apideepseek/plans/refactor-line-gate-targets.txt) phải đảm bảo **dưới 300 dòng**.

### Package `geminiweb`

#### [NEW] [refresher.go](file:///d:/Ds2apideepseek/internal/geminiweb/refresher.go)
*(Thêm file này vào `plans/refactor-line-gate-targets.txt`, đảm bảo file dưới 200 dòng)*
- Định nghĩa sentinel errors:
  - `var ErrUnauthenticated = errors.New("gemini unauthenticated: invalid session or expired cookies")`
  - `var ErrRotationThrottled = errors.New("gemini cookie rotation throttled: rotation attempted too recently")`
- Triển khai `(c *Client) RotateAndInitSession(ctx context.Context, opts RotateSessionOptions) error`:
  - Khóa `c.rotateMu` (single-flight).
  - Kiểm tra throttle 60 giây: Nếu không bật `opts.Force` và `time.Since(c.lastRotated) < 60*time.Second` thì trả về `ErrRotationThrottled`.
  - Gọi xoay cookie qua Google endpoint. Nếu nhận 401 thì trả về `ErrUnauthenticated`.
  - Cập nhật `c.lastRotated = time.Now()`.
  - Gọi `InitSessionWithOptions(ctx, InitSessionOptions{SkipDiscovery: opts.SkipDiscovery})`.
- Triển khai hàm bọc retry:
  - `(c *Client) StreamGenerateWithRetry(ctx context.Context, prompt string, opts GenerateOptions) (*StreamReader, error)`
  - `(c *Client) GenerateWithRetry(ctx context.Context, prompt string, opts GenerateOptions) (*GenerateResult, error)`
  - Nếu gặp lỗi và `errors.Is(err, ErrUnauthenticated)`: thử `RotateAndInitSession(ctx, SkipDiscovery=true)`. Nếu xoay thành công thì thử lại 1 lần duy nhất; nếu bị throttle hoặc unauthenticated thì fail-fast trả về lỗi ngay.
- Triển khai Background Worker:
  - `(r *Runtime) StartBackgroundRefresher(ctx context.Context, store any, interval time.Duration)`:
    - Kiểm tra `config.IsVercel()`: nếu true thì return ngay.
    - Lấy interval từ biến môi trường `GEMINI_COOKIE_REFRESH_INTERVAL` (giây, mặc định 600s, sàn 60s).
    - Ticker chạy định kỳ theo interval: lặp qua các client đang có trong `r.clients`, thêm jitter $\pm 15$ giây giữa các client.
    - Duy trì `failureCount`: nếu lỗi liên tiếp 3 lần thì chuyển sang trạng thái tạm dừng và chỉ log cảnh báo 1 lần duy nhất (tránh log spam).

#### [MODIFY] [auth.go](file:///d:/Ds2apideepseek/internal/geminiweb/auth.go)
- Cập nhật `InitSession`: hỗ trợ tham số `opts ...InitSessionOptions` để có thể bỏ qua `DiscoverModels` khi xoay cookie định kỳ.
- Trong `RotateCookies`: trả về `ErrUnauthenticated` khi status là `401 Unauthorized`.
- Tối ưu `checkSetCookies`: gom các `Set-Cookie` thành map, so sánh với `cookiesMap` hiện tại; chỉ gọi callback `onCookieUpdate` một lần duy nhất khi có sự thay đổi.

#### [MODIFY] [client.go](file:///d:/Ds2apideepseek/internal/geminiweb/client.go)
- Thêm trường `rotateMu sync.Mutex` và `lastRotated time.Time` vào `struct Client`.
- Thêm các trường test seams (`rotateURL`, `appURL`) hỗ trợ mock kiểm thử offline.

#### [MODIFY] [generate.go](file:///d:/Ds2apideepseek/internal/geminiweb/generate.go)
- Trong `StreamGenerate`: bọc lỗi trả về bằng `ErrUnauthenticated` khi status là 401.
- Trong `Generate`: chuyển sang dùng `GenerateWithRetry`.

#### [MODIFY] [runtime_stream.go](file:///d:/Ds2apideepseek/internal/geminiweb/runtime_stream.go)
- Trong [prepareStream](file:///d:/Ds2apideepseek/internal/geminiweb/runtime_stream.go#L259): thay `client.StreamGenerate` bằng `client.StreamGenerateWithRetry`.
- Giữ nguyên số dòng hiện tại (298 dòng, không vượt quá giới hạn 300 dòng).

#### [MODIFY] [runtime.go](file:///d:/Ds2apideepseek/internal/geminiweb/runtime.go)
- Bổ sung phương thức `(r *Runtime) Close()` để dọn dẹp các clients và giải phóng tài nguyên.

---

### Khởi Động Server & Vòng Đời

#### [MODIFY] [main.go](file:///d:/Ds2apideepseek/cmd/ds2api/main.go)
- Khởi động worker nền:
  ```go
  refresherCtx, cancelRefresher := context.WithCancel(context.Background())
  defer cancelRefresher()
  geminiweb.DefaultRuntime().StartBackgroundRefresher(refresherCtx, app.Store, 0)
  ```
- Khi nhận tín hiệu tắt server (SIGINT/SIGTERM):
  - Gọi `cancelRefresher()` để dừng worker nền sạch sẽ.
  - Gọi `geminiweb.DefaultRuntime().Close()` sau khi dừng HTTP server.

---

### Admin API & Documentation

#### [MODIFY] [handler_accounts_testing.go](file:///d:/Ds2apideepseek/internal/httpapi/admin/accounts/handler_accounts_testing.go)
- Thay đổi hành vi nút Test tài khoản Gemini:
  - Thay vì chỉ đọc cache trong bộ nhớ (vốn luôn xanh giả tạo), chủ động gọi `RotateAndInitSession(ctx, Force=true)` để kiểm tra mạng thực tế và làm mới `__Secure-1PSIDTS` ngay trên giao diện.

#### [MODIFY] [plans/refactor-line-gate-targets.txt](file:///d:/Ds2apideepseek/plans/refactor-line-gate-targets.txt)
- Thêm `internal/geminiweb/refresher.go` vào danh sách giám sát giới hạn dòng (< 300 dòng).

#### [MODIFY] [API.md](file:///d:/Ds2apideepseek/API.md) / [API.en.md](file:///d:/Ds2apideepseek/API.en.md)
- Bổ sung tài liệu biến môi trường `GEMINI_COOKIE_REFRESH_INTERVAL` và mô tả hành vi tự động làm mới cookie nền.

---

## Verification Plan

### Automated Tests
1. **Unit Test Toàn Diện Cho Refresher**:
   - Tạo file mới [internal/geminiweb/refresher_test.go](file:///d:/Ds2apideepseek/internal/geminiweb/refresher_test.go) sử `httptest.Server` (hoàn toàn offline, không chạm Google thật):
     - Test 1: Single-flight mutex và Cooldown 60s (chặn 2 request xoay đồng thời).
     - Test 2: SkipDiscovery hoạt động đúng khi xoay cookie định kỳ.
     - Test 3: Batch cookie update chỉ gọi callback lưu Store 1 lần duy nhất.
     - Test 4: On-demand retry thành công khi gặp 401 lần đầu và fail-fast khi xoay thất bại.
     - Test 5: Background refresher dừng sạch khi context bị hủy.
2. **Chạy Toàn Bộ Quality Gates Chuẩn (Bắt buộc theo AGENTS.md)**:
   ```powershell
   # 1. Format code Go
   gofmt -w ./internal/geminiweb/ ./cmd/ds2api/ ./internal/httpapi/admin/accounts/

   # 2. Shell gates chạy qua Git Bash
   & "C:\Program Files\Git\bin\bash.exe" -c "./scripts/lint.sh"
   & "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/check-refactor-line-gate.sh"
   & "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/run-unit-all.sh"

   # 3. Build gate Frontend
   npm run build --prefix webui

   # 4. Build gate Backend
   go build ./...
   ```

### Manual Verification
1. Chạy binary build từ `cmd/ds2api/main.go`:
   - Xác nhận log hiển thị worker đã khởi động thành công với interval cấu hình.
2. Kiểm tra thao tác trên Web UI:
   - Bấm nút **Refresh Token** của tài khoản Gemini: kiểm tra chuỗi `__Secure-1PSIDTS` được cập nhật và ghi đè vào `config.json`.
3. Kiểm tra Graceful Shutdown:
   - Nhấn `Ctrl+C`, kiểm tra log worker dừng sạch sẽ không bị leak goroutine.

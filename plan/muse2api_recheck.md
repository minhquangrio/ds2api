# Kiểm tra lại và sửa Muse2API

Ngày: 07/11/2026. Phạm vi: bản Gemini sửa lại trong working tree, đối chiếu với [review trước](muse2api_review.md). Các sửa lỗi được áp dụng trực tiếp; chưa commit, mở PR hoặc triển khai.

## Kết quả

Các lỗi tái hiện được trong luồng Go/worker đã được sửa và có regression tests. Engine không còn trả câu trả lời giả hoặc lỗi 501 cố định: đã nối kiểm tra session, gửi prompt và đọc assistant bubble theo snapshot upstream. **Chưa có bằng chứng chạy với tài khoản Muse thật**; kết quả offline không xác nhận upstream hiện hoạt động trên host triển khai.

## Đối chiếu các lỗi cũ

| Lỗi | Trạng thái sau sửa |
| --- | --- |
| Engine tạo câu trả lời giả / probe nhận cookie bất kỳ | Engine mặc định; parse đúng hatch_sess, yêu cầu /api/session trả assigned; chat xác nhận VM RUNNING, mở context/thread mới, gửi toàn bộ prompt và đọc DOM. Probe không giả quota. Cần kiểm chứng live. |
| Responses streaming sai schema | Dùng output_text và event discriminator; created/failed có response object; completed có output và usage; tool calls có arguments events. |
| Tool detection / tool_choice bị bỏ qua | Stream và non-stream dùng assistantturn finalization. Request có tools được giữ đến completed trước khi render tool call; tool_choice violation trả lỗi. |
| Reasoning vẫn hiển thị khi tắt | Visible reasoning được lọc, raw reasoning giữ cho detection/archive; tool call trong raw reasoning vẫn được nhận diện. |
| Retry sau khi gửi nội dung | Cả bốn protocol dùng cùng retry runtime và theo dõi response đã commit; giữ partial turn, không chuyển tài khoản khi đã gửi output. |
| current_input_file / inline file dùng DeepSeek | current_input_file giữ nguyên context Muse; inline/stored text chuyển thành text qua xử lý chung. Attachment không đọc được hoặc media chưa hỗ trợ trả lỗi trước worker; không upload bằng DeepSeek. |
| Giới hạn SSE 64 KiB | Parser có giới hạn 10 MiB mỗi dòng, test completed dài 70.000 byte. |
| Thiếu completed vẫn thành công | Phải có completed JSON hợp lệ; EOF, malformed event hoặc snapshot khác nội dung đã phát đều báo lỗi; giữ partial output. Worker cũng từ chối non-stream thiếu completed. |
| Alias trên Vercel đi vào DeepSeek | Go prepare resolve provider trước auth/account/file/session/PoW/lease; trả proxy_to_go. Node relay đã được kiểm tra cùng Go alias regression. |
| Responses mất history/usage | Runtime trả canonical turn và đúng completed object; lưu và GET cùng response/item IDs. Bốn protocol lưu history và usage từ turn; không cộng reasoning hai lần vào tổng token. |
| Python CI thiếu dependencies | Workflow setup Python, cài requirements-dev đã pin; unit gate thu thập toàn bộ worker tests. |

## Lỗi bổ sung đã sửa

- Docker/CLI mặc định mock và Docker bỏ qua lỗi cài Chromium: chuyển mặc định engine, build lỗi nếu cài browser thất bại.
- Cleanup server lấy sai key `driver` thay vì AppKey: driver được đóng khi ứng dụng kết thúc.
- Producer bị bỏ lại khi client ngắt: dùng aclosing và handler_cancellation; test ngắt HTTP khi producer đang chờ xác nhận producer được đóng.
- Browser khởi tạo đồng thời/đang hủy: startup lock và cleanup Playwright; chat/probe dùng cùng account lock.
- Biến thể `-nothinking` bị gửi như tên model upstream: bỏ hậu tố hiển thị trước khi gửi worker; giữ response model của caller.
- Model chat sai loại: từ chối image/video/custom model ở transport chat.
- Thiếu prompt fallback trong turn/usage: dùng cùng prompt thực sự gửi worker.
- tools_enabled=false vẫn giữ tool_choice required ở Chat: bỏ tool_choice cùng tools.
- Retry dùng hết số lần vẫn acquire tài khoản tiếp: dừng đúng giới hạn và giữ outcome cuối.

## Kiểm chứng

- `./scripts/lint.sh`: pass, 0 issues.
- `./tests/scripts/check-refactor-line-gate.sh`: pass, 28 files, không thiếu/vượt giới hạn.
- `./tests/scripts/run-unit-all.sh`: pass; Go suite, 165 Node tests, 18 Python tests.
- `npm run build --prefix webui`: pass; generated assets có cùng hash với bản Gemini đã build.
- `git diff --check`: pass.

Regression Go: [regression_test.go](../internal/muse/regression_test.go), [retry_stream_regression_test.go](../internal/muse/retry_stream_regression_test.go), [Muse file inputs](../internal/httpapi/openai/files/muse_text_inputs_test.go), [Vercel alias](../internal/httpapi/openai/chat/muse_vercel_test.go), [Responses retrieval/history](../internal/httpapi/openai/responses/muse_history_test.go).

Regression worker: [test_engine.py](../workers/muse/test_engine.py), gồm prompt nguyên vẹn, DOM deltas, Stop vẫn hiển thị, session 401, proxy, cleanup và client disconnect. Không dùng credentials thật.

## Giới hạn hiện tại

- Chỉ chat văn bản muse-spark / muse-chat và canonical spark / chat. Ảnh/video/vision chưa được triển khai; các tên model không chứng minh capability upstream.
- Tool requests đợi upstream completed trước khi trả output để bảo đảm detection/policy; plain text vẫn truyền từng phần.
- Authenticated SOCKS5 cần relay riêng; worker báo lỗi, không tự chuyển sang direct.
- Cần thử live trên worker host với tài khoản kiểm thử: session-only cookie, VM wake, prompt nhiều lượt, stream/cancel và proxy đã chọn. Chưa chạy Docker build hoặc deployment Vercel thật.

Hướng dẫn worker: [README](../workers/muse/README.md). Prompt/files/retry/history đã đồng bộ trong [docs/prompt-compatibility.md](../docs/prompt-compatibility.md). Snapshot và giấy phép engine giữ tại [UPSTREAM.md](../workers/muse/UPSTREAM.md).

# Review tích hợp Muse2API

Ngày: 07/11/2026. Phạm vi: thay đổi chưa commit so với HEAD `b5f2eba1c2dd2f5f9f3c4411fdd1a9d6d4705a91`, gồm `internal/muse/`, `workers/muse/`, routing/auth/config, WebUI và scripts.

**Kết luận: chưa sẵn sàng merge hoặc triển khai thực tế.** Driver engine chưa gọi mô hình Muse.ai; các gates hiện tại chỉ chứng minh bộ khung/mock chạy được. Có 11 vấn đề bên dưới cần xử lý, ưu tiên các mục P1.

## 1. [P1] Driver engine tạo phản hồi giả thay vì gọi Muse.ai

Vị trí: [workers/muse/driver_engine.py](../workers/muse/driver_engine.py), dòng 124–139; `probe_account`, dòng 162–173.

Sau `page.goto`, driver tự tạo `Live response from Muse.ai for: ...` rồi phát `completed`; không nhập/gửi prompt hoặc đọc phản hồi upstream. Probe cũng trả `status=ok` cho mọi cookie không rỗng. Docker chọn driver này làm mặc định, nên đây là hành vi của đường chạy được coi là thực.

Tái hiện offline bằng browser stub: chỉ thấy `add_cookies`, `goto`, đóng page/context; vẫn nhận completed và quota active với `not_a_session=fixture`. Cần nối engine thật từ snapshot đã chọn, kiểm chứng session và trả lỗi chưa hỗ trợ cho phần chưa triển khai.

## 2. [P1] Responses stream không tuân thủ schema mà client chờ

Vị trí: [internal/muse/runtime_stream.go](../internal/muse/runtime_stream.go), dòng 90–164, đặc biệt 118–126 và 157–164.

Code phát `response.text.delta`/`response.text.done` thay cho `response.output_text.delta`/`response.output_text.done`, dùng content `type=text` thay cho `output_text`, không có discriminator `type` của event trong JSON. Response cuối không chứa `output` hoặc `usage`. Client dùng event chuẩn sẽ không nhận text đúng và không dựng được kết quả cuối từ response.completed.

Tái hiện bằng recorder: correct delta event=false, JSON event type=false, completed output=false. Cần dùng renderer/schema chuẩn hiện có và bổ sung contract test kiểm tra nội dung event thay vì chỉ kiểm tra chuỗi created/completed xuất hiện.

## 3. [P1] Muse bỏ qua tool-call detection và tool_choice

Vị trí: [internal/muse/client.go](../internal/muse/client.go), dòng 173–187; [internal/muse/stream_chat.go](../internal/muse/stream_chat.go), dòng 139–153.

Cả hai luồng tự dựng Turn từ text và cố định stop reason, không gọi bộ phát hiện/chuẩn hóa tool call hay validate policy chung. EPSE hợp lệ bị trả như content; yêu cầu `tool_choice=required` vẫn có thể thành công mà không có tool call. `finish_reason=content_filter` và output rỗng cũng bị chuyển thành stop thành công.

Tái hiện: XML read_file hợp lệ cho `tool_calls=0`, error=nil dù shared policy từ chối turn; output rỗng/content_filter có stop_reason=stop, turn_error=nil. Cần đưa kết quả worker qua shared assistant-turn finalization, giữ stop reason và áp dụng empty-output retry theo policy chung.

## 4. [P1] Reasoning vẫn hiển thị khi đã tắt

Vị trí: [internal/muse/client.go](../internal/muse/client.go), dòng 178–179; [internal/muse/runtime_stream.go](../internal/muse/runtime_stream.go), dòng 51–54; `runtime_claude.go` dòng 61–76 và `runtime_gemini.go` dòng 29–32.

Request có `Thinking=false` vẫn đưa reasoning vào Thinking và phát reasoning_content/thinking_delta/thought=true. Việc truyền options.thinking xuống worker không đủ để đảm bảo chính sách hiển thị của Go.

Tái hiện với worker trả reasoning dù đã tắt: non-stream Thinking chứa nội dung và OpenAI stream vẫn phát reasoning. Cần giữ raw reasoning cho detection/archive khi phù hợp, nhưng lọc visible reasoning ở lớp chung trước render.

## 5. [P1] Retry streaming sau khi đã gửi nội dung

Vị trí: [internal/muse/retry.go](../internal/muse/retry.go), dòng 108–120; [internal/muse/stream_chat.go](../internal/muse/stream_chat.go), dòng 108–111.

Không kiểm tra byte/content/tool call đã gửi trước khi switch account. StreamChat trả Turn rỗng khi gặp error event nên mất cả partial text trong kết quả lỗi; renderer còn phát role frame trước khi kiểm tra HTTP upstream.

Tái hiện với hai tài khoản: tài khoản thứ nhất phát text rồi 429, tài khoản thứ hai được gọi tiếp; một response có hai role frames, cả hai câu trả lời và hai completion IDs, trong khi lịch sử chỉ nhận text của tài khoản thứ hai. Cần theo dõi trạng thái đã commit/phát output, giữ partial turn khi lỗi và chỉ retry trước khi stream bắt đầu gửi ra client.

## 6. [P1] current_input_file của Muse gọi nhầm upload DeepSeek

Vị trí tích hợp: [internal/httpapi/openai/chat/handler_chat.go](../internal/httpapi/openai/chat/handler_chat.go), dòng 102 và nhánh Muse dòng 210; các entry Responses/Claude/Gemini tương tự. Logic liên quan: [internal/httpapi/openai/history/current_input_file.go](../internal/httpapi/openai/history/current_input_file.go), dòng 43–74.

File preprocessing chạy trước khi vào nhánh Muse. Shared service chỉ loại trừ Gemini và expert, nên khi current_input_file bật và đạt ngưỡng nó upload HISTORY.txt bằng DS client với Muse auth. Request có thể lỗi trước khi tới worker; nếu upload được thì prompt bị thay bằng tham chiếu file worker không đọc được. Inline upload cũng chỉ loại trừ Gemini.

Tái hiện bằng uploader stub: Muse request gọi DeepSeek uploader một lần. Cần capability handling theo provider: giữ ngữ cảnh đầy đủ cho Muse chưa hỗ trợ file, xử lý text input qua path chung và báo lỗi rõ cho attachment chưa hỗ trợ.

## 7. [P2] SSE parser giới hạn mặc định 64 KiB

Vị trí: [internal/muse/stream_chat.go](../internal/muse/stream_chat.go), dòng 58 và 99–100.

bufio.Scanner không được mở rộng buffer. Worker gửi toàn bộ text lần nữa trong completed, nên câu trả lời dài làm parser lỗi dù từng delta nhỏ. Tái hiện với completed chứa 70.000 byte: `bufio.Scanner: token too long`.

Cần dùng SSE reader phù hợp với output dài, giới hạn tài nguyên có chủ đích và regression test lớn cho delta lẫn completed.

## 8. [P1] Stream thiếu completed vẫn được đánh dấu thành công

Vị trí: [internal/muse/stream_chat.go](../internal/muse/stream_chat.go), dòng 116–153; `errorsIsEOF`, dòng 156–157.

Không theo dõi việc đã nhận completed hay chưa và còn coi UnexpectedEOF là kết thúc hợp lệ. Worker đóng stream sau một delta mà chưa hoàn tất vẫn trả error=nil; renderer phát stop/[DONE] và caller ghi lịch sử success.

Tái hiện: stream chỉ có delta `partial` vẫn nhận reports_done=true. Cần yêu cầu terminal event hợp lệ, xử lý EOF trước completed là lỗi và giữ partial content để lưu trạng thái thất bại.

## 9. [P1] Alias Muse trên Vercel đi vào DeepSeek prepare

Vị trí: [internal/js/chat-stream/index.js](../internal/js/chat-stream/index.js), dòng 112–124.

isMuseRequest chỉ biết header hoặc prefix muse-/muse/. Alias tùy chỉnh được Go ResolveModelTarget hỗ trợ nhưng không được Node nhận diện, nên streaming alias đi vào handleVercelStream và DeepSeek prepare/PoW. Khác với đường Go thông thường, đây có thể chọn nhầm tài khoản hoặc lỗi trước khi worker được gọi.

Tái hiện Node entry: muse-spark gọi `?__go=1`; my-muse-alias gọi `?__go=1&__stream_prepare=1`. Cần lấy quyết định provider từ Go resolver trước khi chọn data path, kiểm thử alias và header routing trên Vercel.

## 10. [P2] Responses stream mất nội dung trong lịch sử và usage

Vị trí: [internal/httpapi/openai/responses/responses_handler.go](../internal/httpapi/openai/responses/responses_handler.go), dòng 210–220.

StreamResponsesAPI chỉ trả error. Khi thành công, handler gọi historySession.Success với thinking/text rỗng và usage=nil dù worker đã phát nội dung. Vì vậy archive thiếu câu trả lời và usage ledger không có output thực tế để tính; response stream cũng không đưa completed object vào response store như non-stream.

Cần runtime trả canonical turn/outcome, dùng nó cho lịch sử/usage/response store và cho mọi protocol cùng nhận chính sách retry chung. Hiện Responses/Claude/Gemini gọi ExecuteTurn trực tiếp, trong khi Chat gọi ExecuteTurnWithRetry nên hành vi 429 cũng khác nhau theo protocol.

## 11. [P2] Python gate chưa được chuẩn bị dependency trong CI

Vị trí: [tests/scripts/run-unit-python.sh](../tests/scripts/run-unit-python.sh), dòng 7–10; [tests/scripts/run-unit-all.sh](../tests/scripts/run-unit-all.sh), dòng 9; [.github/workflows/quality-gates.yml](../.github/workflows/quality-gates.yml), job unit-all.

Unit-all đã gọi python -m pytest, nhưng workflow chỉ setup Go/Node và không cài pytest/aiohttp/pytest-asyncio. Môi trường sạch thiếu các package này sẽ lỗi trước khi chạy worker tests; nếu không tìm thấy Python thì script lại bỏ qua gate và thành công.

Cần setup Python và cài dependencies kiểm thử đã khóa trong CI; gate bắt buộc không nên silently skip. Local pass hiện tại dựa vào packages đã có trên máy.

## Kiểm tra đã thực hiện

- `./scripts/lint.sh`: pass, 0 issues.
- `./tests/scripts/check-refactor-line-gate.sh`: pass, 22 files, 0 missing/over_limit.
- `./tests/scripts/run-unit-all.sh`: pass; Go suite, 163 Node tests và 11 Python tests.
- WebUI production build: pass; chuyển outDir sang `.tmp/muse-review-webui` để giữ các build artifact hiện có của bản đang review.
- Ca tái hiện Go độc lập: [.tmp/muse-review/main.go](../.tmp/muse-review/main.go), chạy `go run ./.tmp/muse-review`; config thử nằm trong cùng thư mục tạm, không dùng credentials thật.
- Browser stub cho engine/probe và Node entry stub cho Vercel routing, hoàn toàn offline.

Chưa chạy Muse.ai live. Không sửa mã ứng dụng trong lần review này; chỉ tạo báo cáo và ca tái hiện tạm. Ảnh/video, session renewal và deployment documentation còn chưa có implementation đầy đủ như kế hoạch, nên không nên công bố các capability đó từ scaffold hiện tại.

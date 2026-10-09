# Kế Hoạch Cải Tiến ds2api (v2.2 - Sẵn Sàng Triển Khai)
## Nhận Diện `BardErrorInfo [1095]`, Cooldown Tự Động & Auto-Switch Retry Cho Upstream Gemini Web

> **Mã kế hoạch**: `PLAN-GEMINI-AUTO-RETRY-02.2`  
> **Phiên bản**: `2.2 (Gate-Safe & Verified Against Working Tree)`  
> **Ngày cập nhật**: 09/10/2026  
> **Trạng thái**: Chờ duyệt (Ready for Implementation)  
> **Tài liệu quy chuẩn tuân thủ**: [AGENTS.md](file:///d:/Ds2apideepseek/AGENTS.md), [plans/refactor-line-gate-targets.txt](file:///d:/Ds2apideepseek/plans/refactor-line-gate-targets.txt)

---

## 1. Changelog v2.2 (So Với v2.1)

v2.2 giữ nguyên B1/B2/B3 của v2.1 (đã verify đúng) và sửa 6 điểm còn lại:

| Mã | Vấn đề trong v2.1 | Xử lý trong v2.2 |
| :--- | :--- | :--- |
| **C1** 🔴 | §2.2 nhét hàm `StreamOpenAIChatBuffered` (~77 dòng) vào `runtime_stream.go` — file này **chính xác 296/300 dòng** và **có** trong line-gate → gate FAIL. Bảng §3 ghi "296 → ~298" là bất khả thi. | **Chuyển toàn bộ 3 hàm render stream sang file mới `stream_buffered.go`** (không bị gate). `runtime_stream.go` chỉ còn các helper dùng chung → **giảm** còn ~50 dòng. |
| **C2** 🔴 | Thân hàm `SetAccountCooldownUntil` **biến mất** khỏi v2.1 (v2 có, v2.1 xoá). Đây đúng là chỗ chứa bug `Pool.Reset()` của v1. | Ghi lại đầy đủ vào **§2.4**, kèm ràng buộc cứng: `ReconcileElasticPool` + **KHÔNG `Pool.Reset()`** + log error (không `_ =`). |
| **C3** ✅ | v2.1 bỏ mục "re-upload current-input-file sau switch" của v2. | **Đã kiểm chứng: KHÔNG CẦN THIẾT.** `isGeminiCurrentInputRequest()` khiến `ApplyCurrentInputFile` return sớm cho mọi account Gemini, nên `stdReq.CurrentInputFileApplied` luôn `false` trên đường Gemini → không có gì để re-upload. Xem ghi chú §2.1. |
| **C4** 🟠 | Hàm buffered **mất guard** `thinkBuf.Len()==0 && textBuf.Len()==0`; stream rỗng trả `("", "", nil)` = thành công rỗng, và lỗi rỗng-không-1095 trả `io.EOF` thô. | Khôi phục guard trong `openStreamBuffered` (§2.2) — giữ nguyên message `gemini upstream returned empty output (check proxy/cookies)`. |
| **C5** 🟠 | §2.3 chỉ sửa `chatHistorySession` (chỉ dùng ở OpenAI Chat). Claude và Gemini-native dùng `responsehistory.Session` → vẫn ghi sai account sau switch. | Bổ sung `responsehistory.Session.SetAccountID` + nối `onAccountSwitched` ở **cả 3 handler** (§2.5, §2.7). |
| **C6** 🟡 | M7 nói `min(accountPoolSize, 3)` nhưng code hardcode `maxAttempts := 3`; B4 nói "ghi log cảnh báo" khi 1095 giữa stream nhưng code không log. | `geminiRetryCeiling(store)` tính động từ pool (§2.1); thêm log cảnh báo khi stream bị cắt giữa chừng (§2.2). |

**B1/B2/B3 của v2.1 được giữ nguyên và đã verify:**
- **B1** ✅ [`chathistory.Entry` có `AccountID`](file:///d:/Ds2apideepseek/internal/chathistory/store.go) — chỉ cần thêm field vào `UpdateParams` và gán trong `Store.Update`.
- **B2** ✅ [`Recorder`](file:///d:/Ds2apideepseek/internal/usageledger/recorder.go) có `once sync.Once` nhưng **chưa có `mu`** → thêm `mu` an toàn, không deadlock (`SetAccountID` không gọi `Record`).
- **B3** ✅ `CoolDown` đối xứng với `SwitchAccount`; `internal/auth` không import `geminiweb` → không vòng lặp import.

---

## 2. Thiết Kế Chi Tiết

### 2.1. Bộ điều phối retry dùng chung — `internal/geminiweb/retry.go` (file mới)

```go
package geminiweb

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"time"

	"ds2api/internal/assistantturn"
	"ds2api/internal/auth"
	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

var (
	ErrGeminiThrottled              = errors.New("gemini upstream throttled (BardErrorInfo 1095)")
	ErrAllGeminiAccountsCoolingDown = errors.New("all upstream gemini accounts are cooling down")
)

func IsThrottledError(err error) bool { return errors.Is(err, ErrGeminiThrottled) }

// geminiCooldownDuration đọc thời gian cooldown từ env, mặc định 60s (5x biên an toàn
// so với 12s hồi phục thực nghiệm).
func geminiCooldownDuration() time.Duration {
	if raw := os.Getenv("DS2API_GEMINI_COOLDOWN_SECONDS"); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return 60 * time.Second
}

// geminiRetryCeiling giới hạn số lần thử theo số account Gemini đang schedulable,
// chặn trên ở 5 để tránh vòng lặp bệnh lý. Trả về tối thiểu 1.
func geminiRetryCeiling(store any) int {
	const ceiling = 5
	snap, ok := store.(StoreSnapshotter)
	if !ok {
		return 3
	}
	n := 0
	for _, acc := range snap.Snapshot().Accounts {
		if acc.IsGemini() && acc.IsSchedulable() {
			n++
		}
	}
	if n < 1 {
		return 1
	}
	if n > ceiling {
		return ceiling
	}
	return n
}

// switchAfterThrottle cooldown account hiện tại, evict client cache và chuyển account.
// Trả về false khi không thể chuyển (hết account / account bị ghim / direct token).
func switchAfterThrottle(ctx context.Context, a *auth.RequestAuth, attempt int, op string) bool {
	config.Logger.Warn("[gemini_retry] upstream 1095 throttle detected",
		"op", op, "account", a.AccountID, "attempt", attempt+1)
	a.CoolDown(geminiCooldownDuration())
	DefaultRuntime().EvictClient(a.AccountID)
	return a.SwitchAccount(ctx)
}

// ExecuteTurnWithRetry chạy một turn non-streaming với auto-switch khi bị throttle.
func ExecuteTurnWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	stdReq promptcompat.StandardRequest,
	onAccountSwitched func(newAccountID string),
) (assistantturn.Turn, error) {
	maxAttempts := geminiRetryCeiling(store)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return assistantturn.Turn{}, err
		}

		turn, execErr := ExecuteTurn(ctx, client, stdReq)
		if execErr == nil {
			return turn, nil
		}
		if !IsThrottledError(execErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return assistantturn.Turn{}, execErr
		}
		if !switchAfterThrottle(ctx, a, attempt, "turn") {
			return assistantturn.Turn{}, ErrAllGeminiAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return assistantturn.Turn{}, ErrAllGeminiAccountsCoolingDown
}

// StreamRenderer render một stream cho đúng giao diện của nó.
type StreamRenderer func(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error)

// ExecuteStreamWithRetry chạy một stream với auto-switch khi bị throttle.
//
// Chỉ retry được khi lỗi phát sinh TRƯỚC byte đầu tiên xuống w — điều này do
// openStreamBuffered bảo đảm (§2.2): nó đọc chunk đầu thành công rồi mới ghi header SSE.
func ExecuteStreamWithRetry(
	ctx context.Context,
	a *auth.RequestAuth,
	store any,
	stdReq promptcompat.StandardRequest,
	w http.ResponseWriter,
	render StreamRenderer,
	onAccountSwitched func(newAccountID string),
) (string, string, error) {
	maxAttempts := geminiRetryCeiling(store)

	for attempt := 0; attempt < maxAttempts; attempt++ {
		client, err := DefaultRuntime().GetClient(ctx, a.Account, store)
		if err != nil {
			return "", "", err
		}

		thinking, text, streamErr := render(ctx, client, stdReq, w)
		if streamErr == nil {
			return thinking, text, nil
		}
		if !IsThrottledError(streamErr) || !a.UseConfigToken || a.TargetAccount != "" {
			return thinking, text, streamErr
		}
		if !switchAfterThrottle(ctx, a, attempt, "stream") {
			return "", "", ErrAllGeminiAccountsCoolingDown
		}
		if onAccountSwitched != nil {
			onAccountSwitched(a.AccountID)
		}
	}
	return "", "", ErrAllGeminiAccountsCoolingDown
}
```

> **Ghi chú C3 — vì sao không cần re-upload current-input-file:**
> [`isGeminiCurrentInputRequest(a)`](file:///d:/Ds2apideepseek/internal/httpapi/openai/history/current_input_file.go) trả `true` cho mọi account Gemini, và `ApplyCurrentInputFile` return sớm ngay sau đó → `stdReq.CurrentInputFileApplied` **luôn `false`** trên đường Gemini (đã có test khẳng định: `history_split_test.go` — *"expected CurrentInputFileApplied to be false for gemini"*). Vì vậy sau khi switch account không có file nào cần upload lại. Nếu sau này Gemini được cấp tính năng current-input-file, **phải** bổ sung re-upload vào `ExecuteTurnWithRetry`/`ExecuteStreamWithRetry` — ghi chú này để không bị bỏ sót.

---

### 2.2. Stream buffered — `internal/geminiweb/stream_buffered.go` (file mới)

**Lý do tách file:** `internal/geminiweb/runtime_stream.go` hiện **chính xác 296 dòng** trên trần 300 và **có** trong `plans/refactor-line-gate-targets.txt`. Thêm bất kỳ hàm nào vào đó đều làm `check-refactor-line-gate.sh` FAIL. Ba hàm render được **chuyển hẳn** sang file mới (không nằm trong danh sách gate); `runtime_stream.go` chỉ giữ `prepareStream`, `setSSEHeaders`, `sendClaudeEvent`, `emitSSEJSON`.

```go
package geminiweb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ds2api/internal/config"
	"ds2api/internal/promptcompat"
)

// openStreamBuffered mở StreamGenerate và đọc chunk đầu tiên TRƯỚC khi ghi bất kỳ
// byte nào xuống w, để lỗi 1095 ở đầu stream vẫn retry được sang account khác.
func openStreamBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest) (*StreamReader, *ParsedChunk, error) {
	reader, err := prepareStream(ctx, client, stdReq)
	if err != nil {
		return nil, nil, err
	}

	chunk, err := readFirstChunk(reader)
	if err != nil {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] close after failed pre-read", "error", closeErr)
		}
		return nil, nil, err
	}
	return reader, chunk, nil
}

// readFirstChunk lấy chunk dữ liệu đầu tiên, đồng thời phân loại lỗi rỗng/throttle.
func readFirstChunk(reader *StreamReader) (*ParsedChunk, error) {
	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if reader.parser.LastErrorCode() == 1095 {
				return nil, ErrGeminiThrottled
			}
			if errors.Is(readErr, io.EOF) {
				// Giữ nguyên message chẩn đoán cũ (C4) — không trả io.EOF thô.
				return nil, errors.New("gemini upstream returned empty output (check proxy/cookies)")
			}
			return nil, readErr
		}
		if chunk == nil {
			continue
		}
		if reader.parser.LastErrorCode() == 1095 && !chunk.IsFinished {
			return nil, ErrGeminiThrottled
		}
		return chunk, nil
	}
}

// StreamOpenAIChatBuffered render một turn Gemini thành SSE OpenAI chat.
func StreamOpenAIChatBuffered(ctx context.Context, client *Client, stdReq promptcompat.StandardRequest, w http.ResponseWriter) (string, string, error) {
	reader, firstChunk, err := openStreamBuffered(ctx, client, stdReq)
	if err != nil {
		return "", "", err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			config.Logger.Warn("[geminiweb] stream close failed", "error", closeErr)
		}
	}()

	setSSEHeaders(w)
	flusher, canFlush := w.(http.Flusher)
	id := fmt.Sprintf("chatcmpl-gemini-%d", time.Now().UnixNano())
	created := time.Now().Unix()
	emitChunk := func(delta map[string]any, finish any) {
		emitSSEJSON(w, map[string]any{
			"id": id, "object": "chat.completion.chunk", "created": created, "model": stdReq.ResponseModel,
			"choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}},
		})
		if canFlush {
			flusher.Flush()
		}
	}

	var thinkBuf, textBuf strings.Builder
	emitChunk(map[string]any{"role": "assistant"}, nil)

	writeChunk := func(chunk *ParsedChunk) {
		if chunk.ThoughtDelta != "" {
			thinkBuf.WriteString(chunk.ThoughtDelta)
			emitChunk(map[string]any{"reasoning_content": chunk.ThoughtDelta}, nil)
		}
		if chunk.TextDelta != "" {
			textBuf.WriteString(chunk.TextDelta)
			emitChunk(map[string]any{"content": chunk.TextDelta}, nil)
		}
	}

	writeChunk(firstChunk)

	for {
		chunk, readErr := reader.ReadChunk()
		if readErr != nil {
			if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
				return thinkBuf.String(), textBuf.String(), readErr
			}
			break
		}
		if chunk == nil {
			continue
		}
		writeChunk(chunk)
		if chunk.IsFinished {
			break
		}
	}

	// 1095 đến giữa stream: không thể retry (đã gửi byte), nhưng KHÔNG được im lặng (C6).
	if reader.parser.LastErrorCode() == 1095 {
		config.Logger.Warn("[geminiweb] stream truncated by upstream throttle (1095)",
			"account", stdReq.ResponseModel, "thinking_len", thinkBuf.Len(), "text_len", textBuf.Len())
		return thinkBuf.String(), textBuf.String(),
			errors.New("gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated")
	}

	if thinkBuf.Len() == 0 && textBuf.Len() == 0 {
		return "", "", errors.New("gemini upstream returned empty output (check proxy/cookies)")
	}

	emitChunk(map[string]any{}, "stop")
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
	return thinkBuf.String(), textBuf.String(), nil
}
```

**`StreamGeminiContentBuffered` và `StreamClaudeMessagesBuffered`:** cùng khuôn chính xác như trên, chỉ thay phần thân emit:
- **Gemini native:** thay `setSSEHeaders` + `emitGemini(parts, finish)` (giữ nguyên như bản cũ trong `runtime_stream.go`), kết thúc bằng `emitGemini([]any{map[string]any{"text": ""}}, "STOP")`.
- **Claude Messages:** thay bằng `message_start` / `content_block_*` / `message_delta` / `message_stop` — **giữ nguyên 100% logic block đang có** trong `StreamClaudeMessages` hiện tại; chỉ khác là mọi thứ được ghi **sau** khi `openStreamBuffered` trả về thành công, và `firstChunk` được nạp bằng đúng đoạn `writeChunk` đang có.

Cả hai bản đều phải giữ: (a) vòng `if chunk.IsFinished { break }`, (b) guard rỗng cuối hàm, (c) cảnh báo 1095-giữa-stream.

> **Hệ quả lên `runtime_stream.go`:** xoá 3 hàm `StreamOpenAIChat` / `StreamGeminiContent` / `StreamClaudeMessages` khỏi file này (đã chuyển sang `stream_buffered.go`), giữ lại `prepareStream`, `setSSEHeaders`, `sendClaudeEvent`, `emitSSEJSON` → file còn **~50/300 dòng**.

---

### 2.3. Nhận diện `BardErrorInfo` tại Parser

#### `internal/geminiweb/stream_parser.go` (244 → ~258 dòng, trần 300 ✅)

Thêm trường + accessor vào `StreamParser`:
```go
type StreamParser struct {
	// ... các trường hiện tại
	lastErrorCode int
	hasErrorInfo  bool
}

func (p *StreamParser) LastErrorCode() int { return p.lastErrorCode }
func (p *StreamParser) HasErrorInfo() bool { return p.hasErrorInfo }
```

Thêm bộ nhận diện, dùng lại helper có sẵn `nestedValue` và `toInt` của package:
```go
const bardErrorInfoType = "type.googleapis.com/assistant.boq.bard.application.BardErrorInfo"

// detectBardError ghi nhận mã lỗi BardErrorInfo trong một envelope "wrb.fr".
//
// Envelope lỗi thật (env[2] == null):
//   ["wrb.fr",null,null,null,null,[8,null,[["type...BardErrorInfo",[1095]]]]]
// nên KHÔNG thể đi qua nhánh đọc env[2] hiện có — phải quét riêng env[5][2][0].
func (p *StreamParser) detectBardError(env []any) {
	if len(env) < 6 {
		return
	}
	info, ok := nestedValue(env, 5, 2, 0).([]any)
	if !ok {
		return
	}
	if typ, _ := nestedValue(info, 0).(string); typ != bardErrorInfoType {
		return
	}
	if code, ok := toInt(nestedValue(info, 1, 0)); ok {
		p.lastErrorCode = code
		p.hasErrorInfo = true
	}
}
```

Trong `parseFrame`, gọi **trước** nhánh `env[2]` (vì envelope lỗi có `env[2] == null` và hiện bị `continue` bỏ qua):
```go
for _, env := range envelopes {
	if len(env) < 3 {
		continue
	}
	p.detectBardError(env)          // ← thêm dòng này

	innerStr, ok := env[2].(string) // phần còn lại giữ nguyên
	if !ok || innerStr == "" {
		continue
	}
	// ...
}
```

> **Ràng buộc cứng:** `detectBardError` **tuyệt đối không** trả error và `parseFrame` **tuyệt đối không** `return nil, err` cho nhánh này. Vòng lặp `Feed()` nuốt error và chỉ ghi log Warn → nhánh đó sẽ là **no-op hoàn toàn** (đây chính là bẫy của v1).

#### `internal/geminiweb/generate.go` (258 → ~268 dòng, trần 300 ✅)

```go
	if reader.parser.LastErrorCode() == 1095 && (lastChunk == nil || !lastChunk.IsFinished) {
		return nil, ErrGeminiThrottled
	}
	if lastChunk == nil {
		// Rỗng nhưng KHÔNG phải 1095 (cookie hết hạn, trang chặn, proxy chết...):
		// giữ nguyên lỗi gốc, tuyệt đối không gắn ErrGeminiThrottled để tránh cooldown oan.
		return nil, errors.New("empty response from gemini generate")
	}
```

> **Giới hạn đã biết:** `Generate()` `break` ngay khi gặp `chunk.IsFinished`, nên nếu frame 1095 đến **sau** frame finished thì không bao giờ được đọc. Ca này để lại nguyên trạng (trả text như thành công) — chấp nhận, vì trên thực tế Google trả frame lỗi *thay cho* nội dung. Ghi lại đây để người sau không tưởng là đã phủ hết.

---

### 2.4. Cooldown trong Auth Resolver — `internal/auth/request.go` (606 → ~625 dòng, không bị gate)

```go
// CoolDown đặt cooldown cho account hiện tại. Đối xứng với SwitchAccount,
// để package geminiweb không cần chạm vào resolver (trường unexported).
func (a *RequestAuth) CoolDown(d time.Duration) bool {
	if a == nil || a.resolver == nil || !a.UseConfigToken || a.AccountID == "" || d <= 0 {
		return false
	}
	a.resolver.SetAccountCooldownUntil(a, d)
	return true
}

// SetAccountCooldownUntil ghi cooldown_until vào config store. Mirror đúng
// persistCooldownUntil của deepseek/client/client_auth.go.
func (r *Resolver) SetAccountCooldownUntil(a *RequestAuth, d time.Duration) {
	if r == nil || a == nil || !a.UseConfigToken || a.AccountID == "" || d <= 0 {
		return
	}
	until := float64(time.Now().Add(d).Unix())
	identifier := a.AccountID

	if err := r.Store.Update(func(c *config.Config) error {
		for i := range c.Accounts {
			if c.Accounts[i].Identifier() == identifier {
				c.Accounts[i].CooldownUntil = until
				break
			}
		}
		if c.ElasticPool.Enabled {
			account.ReconcileElasticPool(c)
		}
		return nil
	}); err != nil {
		config.Logger.Error("[gemini_cooldown] failed to persist cooldown_until",
			"account", identifier, "error", err)
		return
	}

	a.Account.CooldownUntil = until

	// KHÔNG gọi r.Pool.Reset() ở đây.
	// Pool.Reset() xoá p.inUse (phá vỡ đếm lease đang in-flight) và
	// drainWaitersLocked() (thundering herd). Không cần thiết: Pool.tryAcquire()
	// đọc store.FindAccount() tươi ở mỗi lần acquire rồi tự kiểm tra IsSchedulable().
	config.Logger.Warn("[gemini_cooldown] account cooling down after upstream throttle",
		"account", identifier, "duration", d.String(), "until", until)
}
```

> `request.go` đã import sẵn `account`, `config`, `time` → **không cần thêm import**.
> `Store.Update` **không** tự reconcile pool → dòng `ReconcileElasticPool` là bắt buộc, nếu thiếu thì account standby không được promote thay chỗ account bị bench.

---

### 2.5. Ghi đúng account vào History & Usage Ledger

#### a) `internal/chathistory/store.go` — thêm field và áp dụng

```go
type UpdateParams struct {
	AccountID        string   // ← account thực tế đã hoàn thành request (sau switch)
	Status           string
	// ... các trường còn lại giữ nguyên
}
```
Trong `Store.Update`, thêm cạnh nhánh `Status`:
```go
	if id := strings.TrimSpace(params.AccountID); id != "" {
		item.AccountID = id
	}
```
(`Entry.AccountID` đã tồn tại sẵn ở `store.go`; `rebuildIndexLocked()` phía dưới tự cập nhật index.)

#### b) `internal/usageledger/recorder.go` — setter thread-safe

```go
type Recorder struct {
	store     *Store
	mu        sync.Mutex // ← thêm mới, bảo vệ meta.AccountID
	meta      Meta
	startedAt time.Time
	once      sync.Once
}

func (r *Recorder) SetAccountID(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.meta.AccountID = strings.TrimSpace(id)
}
```
Trong `Record`, đọc account dưới lock:
```go
	r.mu.Lock()
	accountID := strings.TrimSpace(r.meta.AccountID)
	r.mu.Unlock()
	// ... dùng accountID khi dựng Record ở dòng AccountID:
```
> Không deadlock: `SetAccountID` không gọi `Record`, và `Record` không giữ `mu` khi chạm `store.mu`.

#### c) `internal/httpapi/openai/chat/chat_history.go` (329 → ~340 dòng)

```go
func (s *chatHistorySession) updateAccountID(accountID string) {
	if s == nil {
		return
	}
	s.startParams.AccountID = strings.TrimSpace(accountID)
	if s.store != nil && s.entryID != "" {
		s.persistUpdate(chathistory.UpdateParams{AccountID: accountID})
	}
	if s.ledger != nil {
		s.ledger.SetAccountID(accountID)
	}
}
```

#### d) `internal/responsehistory/session.go` — mirror y hệt (C5)

`responsehistory.Session` có cùng cấu trúc (`store`, `ledger`, `entryID`, `startParams`, `persistUpdate`) nên thêm phương thức tương đương:
```go
func (s *Session) SetAccountID(accountID string) {
	if s == nil {
		return
	}
	s.startParams.AccountID = strings.TrimSpace(accountID)
	if s.store != nil && s.entryID != "" {
		s.persistUpdate(chathistory.UpdateParams{AccountID: accountID})
	}
	if s.ledger != nil {
		s.ledger.SetAccountID(accountID)
	}
}
```
Cần cho **Claude Messages** và **Gemini Native** — hai surface này dùng `responsehistory.Session`, không dùng `chatHistorySession`.

---

### 2.6. `EvictClient` với Grace Period — `internal/geminiweb/runtime.go` (199 → ~215 dòng, trần 300 ✅)

```go
// EvictClient xoá client khỏi cache và đóng TRỄ sau 30s, để các request đồng thời
// khác đang giữ con trỏ client đọc hết body an toàn mà không rò rỉ connection pool.
// Khác InvalidateClient: InvalidateClient gọi Close() ngay → cắt ngang stream đang chạy.
func (r *Runtime) EvictClient(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		id = "default"
	}

	r.mu.Lock()
	existing, found := r.clients[id]
	if found {
		delete(r.clients, id)
	}
	r.mu.Unlock()

	if found && existing != nil {
		time.AfterFunc(30*time.Second, func() {
			if err := existing.Close(); err != nil {
				config.Logger.Warn("[geminiweb] deferred client close failed", "id", id, "error", err)
			}
		})
	}
}
```
> `runtime.go` phải thêm import `time`.

---

### 2.7. Nối vào 3 Ingress Adapters

Cả 3 handler đều có sẵn `h.Store` (config store), `h.DS`, `h.Auth`.

**1. OpenAI Chat** — `internal/httpapi/openai/chat/handler_chat.go`
- Non-stream: thay `geminiweb.ExecuteTurn(...)` bằng
  ```go
  turn, err := geminiweb.ExecuteTurnWithRetry(r.Context(), a, h.Store, stdReq, func(string) {
      if historySession != nil {
          historySession.updateAccountID(a.AccountID)
      }
  })
  ```
- Stream: thay `geminiweb.StreamOpenAIChat(...)` bằng
  ```go
  thinking, text, streamErr := geminiweb.ExecuteStreamWithRetry(
      r.Context(), a, h.Store, stdReq, w, geminiweb.StreamOpenAIChatBuffered,
      func(string) {
          if historySession != nil {
              historySession.updateAccountID(a.AccountID)
          }
      })
  ```
- Ánh xạ lỗi hết account (M5): nếu `errors.Is(err, geminiweb.ErrAllGeminiAccountsCoolingDown)` → `writeOpenAIError(w, http.StatusTooManyRequests, ...)` với body chuẩn OpenAI `code: "all_gemini_accounts_cooling_down"`.

**2. Claude Messages** — `internal/httpapi/claude/handler_messages.go`
- Dùng `responsehistory.Session.SetAccountID` trong callback, và `StreamClaudeMessagesBuffered` cho nhánh stream.
- Lỗi hết account → `429` qua `writeClaudeError`.

**3. Gemini Native** — `internal/httpapi/gemini/handler_generate.go`
- Dùng `responsehistory.Session.SetAccountID`, và `StreamGeminiContentBuffered`.
- Lỗi hết account → `429` qua `writeGeminiError`.

**4. Vercel path** — `internal/httpapi/openai/chat/vercel_stream.go`
- Đây là đường `?__stream_prepare=1` / `?__stream_release=1`. Hiện `vercel_stream.go` **đã** tự gọi `a.SwitchAccount(...)` và `ReuploadAppliedCurrentInputFile` cho nhánh của nó; không đổi hành vi. Chỉ cần xác nhận bằng test rằng luồng prepare/release không bị ảnh hưởng bởi việc hoãn `setSSEHeaders` (vì prepare/release không đi qua `StreamOpenAIChatBuffered`).

---

## 3. Bản Đồ Tệp & Line Gate

| Tệp tin | Hiện tại | Sau sửa | Ngưỡng | Trạng thái |
| :--- | ---: | ---: | ---: | :---: |
| `internal/geminiweb/stream_parser.go` | 244 | ~258 | 300 | ✅ |
| `internal/geminiweb/generate.go` | 258 | ~268 | 300 | ✅ |
| `internal/geminiweb/runtime.go` | 199 | ~215 | 300 | ✅ |
| `internal/geminiweb/runtime_stream.go` | **296** | **~50** (chuyển 3 hàm render đi) | 300 | ✅ |
| `internal/geminiweb/stream_buffered.go` | *mới* | ~230 | **không gate** | ✅ |
| `internal/geminiweb/retry.go` | *mới* | ~150 | **không gate** | ✅ |
| `internal/auth/request.go` | 606 | ~625 | không gate | ✅ |
| `internal/responsehistory/session.go` | — | +~12 | không gate | ✅ |
| `internal/chathistory/store.go` | 836 | ~842 | không gate | ✅ |
| `internal/usageledger/recorder.go` | 119 | ~132 | không gate | ✅ |
| `internal/httpapi/openai/chat/chat_history.go` | 329 | ~340 | không gate | ✅ |
| `internal/httpapi/openai/chat/handler_chat.go` | 404 | ~415 | không gate | ✅ |
| `internal/httpapi/claude/handler_messages.go` | 278 | ~290 | không gate | ✅ |
| `internal/httpapi/gemini/handler_generate.go` | 237 | ~250 | không gate | ✅ |

> Chỉ `internal/geminiweb/*.go` nằm trong `plans/refactor-line-gate-targets.txt`. Việc tách hàm render sang file mới là refactor **bắt buộc để qua gate** — được `AGENTS.md` cho phép ("unless they are required to make the change pass gates").

---

## 4. Kiểm Thử & Quality Gates

### 4.1. Unit tests
1. **`internal/geminiweb/stream_parser_test.go` — `TestStreamParserBardErrorInfo1095`**
   Feed đúng frame thật của Google (4 frame như §1.2 của v1): khẳng định `Feed()` **không** trả error, `HasErrorInfo() == true`, `LastErrorCode() == 1095`.
   Thêm ca âm: frame có `env[2]` là string bình thường → `HasErrorInfo() == false`.
2. **`internal/geminiweb/generate_test.go`**
   - `LastErrorCode == 1095` + `lastChunk == nil` → `ErrGeminiThrottled`.
   - `LastErrorCode == 0` + `lastChunk == nil` → lỗi **giữ nguyên** `empty response from gemini generate` (khẳng định `!IsThrottledError(err)`).
3. **`internal/auth/request_test.go` — `TestRequestAuthCoolDown`**
   Gọi `a.CoolDown(60s)` rồi khẳng định hành vi: `acc.IsCoolingDown() == true`, `acc.IsSchedulable() == false`. (Kiểm tra qua hành vi, **không** assert field private của package `account`.)
4. **`internal/geminiweb/retry_test.go`**
   - Non-stream: account 1 trả 1095 → switch account 2 → thành công; khẳng định account 1 đã vào cooldown.
   - Stream: chunk đầu tiên dính 1095 → switch và stream thành công **và** `w` không nhận byte nào ở lần thử 1.
   - Toàn bộ pool cooling down → `ErrAllGeminiAccountsCoolingDown`.
   - Giữa stream dính 1095 → trả lỗi truncation, **không** retry.
5. **`internal/chathistory/store_test.go` / `internal/usageledger/recorder_test.go`**
   `UpdateParams{AccountID: "b"}` cập nhật đúng `Entry.AccountID`; `SetAccountID` trước `Record` ghi đúng account vào ledger.

### 4.2. Gates bắt buộc (Windows Git Bash)
```powershell
gofmt -w (mọi file .go đã đổi)
& "C:\Program Files\Git\bin\bash.exe" -c "./scripts/lint.sh"
& "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/check-refactor-line-gate.sh"
& "C:\Program Files\Git\bin\bash.exe" -c "./tests/scripts/run-unit-all.sh"
npm run build --prefix webui
```

### 4.3. Tài liệu (M1)
- `API.md` + `API.en.md`: mục cơ chế chống nghẽn upstream, biến `DS2API_GEMINI_COOLDOWN_SECONDS` (mặc định 60s), mã lỗi `all_gemini_accounts_cooling_down`.
- `API.md` + `API.en.md`: **thay đổi hành vi client-visible cần ghi rõ** — stream bị 1095 giữa chừng giờ trả **lỗi truncation** (`gemini upstream throttled mid-stream (BardErrorInfo 1095); output truncated`) thay vì kết thúc sạch bằng `finish_reason: "stop"` với nội dung cụt. Client cần biết để không coi output cụt là hoàn chỉnh. (AGENTS.md yêu cầu đồng bộ tài liệu khi hành vi người dùng thay đổi.)
- `docs/DEPLOY.md`: cách xử lý khi nhiều account cùng proxy IP bị 1095.
- **Không** sửa `docs/prompt-compatibility.md` — thay đổi này không chạm vào luồng chuẩn hoá prompt/tool/history-split.

---

## 5. Triển Khai Lên VPS (`15.235.142.108`)

Service chạy **binary trần** tại `/www/wwwroot/ds2api/ds2api` (không qua Docker).

1. Đóng gói: chạy `build_deploy.bat` → build WebUI + binary Linux amd64 + đóng `ds2api_deploy.zip` (gồm `ds2api`, `static/`, `config.json`).
2. Upload:
   - `ds2api` → `/www/wwwroot/ds2api/ds2api`
   - **`static/`** → `/www/wwwroot/ds2api/static/` (bắt buộc nếu WebUI có thay đổi — gate có `npm run build --prefix webui`)
3. Restart tiến trình.
4. Kiểm tra: chạy kịch bản dịch dồn dập và đối chiếu trang **Lịch sử & Token** — xác nhận không còn 502 `empty response from gemini generate`, và entry lỗi (nếu còn) hiển thị **đúng account** đã gây lỗi.

> Chưa verify được độc lập phần này (không SSH được trong phiên review). Cần xác nhận lại đường dẫn + cách restart trước khi upload.

---

## 6. Rủi Ro & Giảm Thiểu

| Rủi ro | Mức | Giảm thiểu |
| :--- | :---: | :--- |
| Throttle theo **IP proxy** — đổi account vẫn dính 1095 nếu cùng IP | **Cao** | Log cảnh báo khi ≥2 account cùng proxy bị 1095 trong 60s. **Đây là giảm thiểu thủ công (log + phân tách proxy trong `config.json`), không phải tự động.** Nếu thực tế cho thấy throttle theo IP, auto-switch sẽ chỉ đốt hết pool rồi trả 429 — phải xử lý ở tầng proxy. |
| 1095 đến giữa stream | Trung bình | Không retry (không thể ghép lại sạch); trả lỗi truncation tường minh + log Warn. Client biết là output không đầy đủ. |
| `EvictClient` đóng trễ 30s | Thấp | `time.AfterFunc`; client mới cho cùng id được tạo độc lập, không ảnh hưởng. |
| Vòng lặp retry khi mọi account đều bị phạt | Thấp | `geminiRetryCeiling()` chặn trên 5 và thoát ngay khi `SwitchAccount()` trả `false`. |
| Account bị ghim (`TargetAccount != ""`) | Thấp | Bỏ qua auto-switch, trả lỗi trực tiếp — tôn trọng chỉ định của caller. |
| Direct-token caller (`UseConfigToken == false`) | Thấp | `CoolDown` và `SwitchAccount` đều no-op → trả lỗi gốc, không cooldown. |
| Cooldown 60s quá ngắn/dài | Thấp | Cấu hình qua `DS2API_GEMINI_COOLDOWN_SECONDS`; theo dõi log `[gemini_cooldown]` để chỉnh. |

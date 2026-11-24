# HTTP

Đặc tả gọi HTTP ra bên ngoài qua thư viện `"kitwork/http"`.

## 1. Tổng quan

Trong `.work`, không dùng `fetch()` bất đồng bộ mà dùng chuỗi mô tả đồng bộ của `"kitwork/http"`: **Mô tả đứng trước, `.send()` đứng cuối, và kiểm tra kết quả qua `result.ok`.**

```js
import * as http from "kitwork/http";

const response = http
  .post("https://api.vietqr.io/v2/generate")
  .headers({
    "x-client-id": env.VIETQR_CLIENT_ID,
    "x-api-key": env.VIETQR_API_KEY,
  })
  .timeout("5s")
  .retry(2)
  .json({
    accountNo: "113366668888",
    accountName: "CONG TY KITWORK",
    acqId: 970415,
    amount: 150000,
  })
  .send();

if (!response.ok) {
  return context.status(502).json({
    code: response.code,
    message: "Không tạo được mã thanh toán",
  });
}

const payload = response.data;
```

---

## 2. Khởi tạo và Chỉnh sửa yêu cầu

### 2.1. Phương thức khởi tạo

- `http.get(url)`
- `http.post(url)`
- `http.put(url)`
- `http.patch(url)`
- `http.delete(url)`
- `http.head(url)`

*(Chỉ chấp nhận scheme `https://` hoặc `http://`.)*

### 2.2. Các bước chỉnh sửa (đứng trước `.send()`)

| Phương thức | Ý nghĩa |
|---|---|
| `.query({ key: value })` | Ghép thêm tham số query vào URL (tự động mã hoá URL; bỏ qua khoá có giá trị `undefined` hoặc `null`) |
| `.header(name, value)` / `.headers({ ... })` | Đặt header cho yêu cầu gửi đi |
| `.bearer(token)` | Đặt `Authorization: Bearer <token>` |
| `.basic(username, password)` | Đặt `Authorization: Basic <base64>` |
| `.json(value)` | Đặt thân yêu cầu là JSON và tự gắn `Content-Type: application/json` |
| `.form({ key: value })` | Đặt thân yêu cầu dạng `application/x-www-form-urlencoded` |
| `.text(str)` | Đặt thân yêu cầu dạng văn bản thuần |
| `.timeout("5s")` | Thời gian tối đa cho cả yêu cầu, kể cả các lần thử lại (mặc định `"10s"`, trần `"60s"`); không vượt thời gian của lượt chạy gọi `.send()` |
| `.retry(count)` | Số lần thử lại thêm khi gặp lỗi mạng tạm thời hoặc mã `502`/`503`/`504`/`429` (mặc định `0`, trần `5`) |
| `.cache("5m")` | Giữ kết quả thành công trong RAM cho các yêu cầu `GET` có cùng URL và header |
| `.persist("24h")` | Giữ kết quả thành công trên đĩa (`.database/disk/`) cho các yêu cầu `GET` |

---

## 3. Thực thi (`.send()`) và Cấu trúc kết quả

- Yêu cầu chỉ thực sự đi ra mạng khi gọi **`.send()`** ở cuối chuỗi.
- `.send()` không bao giờ ném lỗi vì mạng đứt hay vì máy chủ đích trả về `4xx`/`5xx`; mọi kết quả mạng đều trả về một object chuẩn. Riêng khi lượt chạy gọi nó kết thúc lúc đang chờ (request bị huỷ, hết giờ), `.send()` là lỗi `CANCELLED`, kết thúc lượt chạy:

| Thuộc tính | Kiểu | Ý nghĩa |
|---|---|---|
| `response.ok` | `boolean` | `true` khi nhận được mã HTTP `200..299` và giải mã thân thành công; `false` trong mọi trường hợp khác |
| `response.status` | `number \| null` | Mã trạng thái HTTP (`200`, `404`, `500`…), hoặc `null` nếu chưa nhận được phản hồi HTTP (lỗi DNS, timeout) |
| `response.code` | `string \| null` | `null` khi `ok: true`; khi `ok: false` là một trong: `"TIMEOUT"`, `"NETWORK"`, `"DENIED"` (bị chặn SSRF), `"TOO_LARGE"`, `"INVALID_RESPONSE"`, hoặc `"HTTP_4XX"` / `"HTTP_5XX"` |
| `response.error` | `string \| null` | Mô tả lỗi ngắn gọn khi `ok: false` |
| `response.data` | `any` | Dữ liệu thân phản hồi đã tự động giải mã (object/mảng nếu `Content-Type` là JSON, chuỗi nếu là văn bản) |
| `response.text` | `string` | Thân phản hồi dạng chuỗi nguyên bản |
| `response.headers` | `object` | Các header của phản hồi (khoá đã chuyển về chữ thường) |

---

## 4. Chặn SSRF và Giới hạn an toàn

1. **Chặn SSRF ở tầng kết nối (`DialContext`):**
   - Engine kiểm tra địa chỉ IP sau khi phân giải DNS (chống DNS Rebinding) và **tự động từ chối (`code: "DENIED"`) mọi kết nối tới địa chỉ nội bộ / riêng tư**: loopback (`127.0.0.0/8`, `::1`), mạng LAN (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`), link-local và địa chỉ metadata đám mây (`169.254.169.254`, `fe80::/10`).
   - Mọi bước chuyển hướng (`301`, `302`, `307`, `308`, tối đa 5 bước) đều được kiểm tra lại địa chỉ IP đích và tự động gỡ header `Authorization` / `Cookie` khi chuyển sang domain khác.
2. **Trần kích thước phản hồi:**
   - Thân phản hồi được đọc có giới hạn (mặc định tối đa `10 MiB`); vượt trần dừng đọc ngay và trả về `{ ok: false, code: "TOO_LARGE" }`.
3. **Không gọi HTTP ở cấp file lúc nạp:**
   - `.send()` chỉ được gọi bên trong handler của route, việc của trang/component, routine, queue job hoặc schedule.


---

## Trạng thái triển khai (16/11/2026)

engine2 làm các mục trên (`library/http`). Engine chọn thêm: `data` của thân không phải JSON là chữ, không có thân là `null`,
và vẫn đọc khi `HTTP_4XX`, `HTTP_5XX`; `error` là một câu ngắn ("the server answered 404 Not Found"); `.timeout` tính cả các
lần chạy lại; `.maxBytes("…")` đổi trần thân (mặc định 10 MiB, tối đa 64 MiB); POST, PATCH chạy lại phải có header
`Idempotency-Key`, thiếu là lỗi; `.persist` giữ trên đĩa qua `disk()` của `kitwork/storage`. Lỗi của chương trình (URL không
phải `http(s)`, thân trên GET, `.send(data)`, `.timeout` quá 60 giây) vẫn bay lên. Từ 15/11 `send()` và các lần thử lại kết thúc
cùng lượt chạy (request, routine, job); lượt chạy đã hết thì `send()` là lỗi `CANCELLED` (`library/http/request.go`).

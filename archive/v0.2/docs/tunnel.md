# Tunnel

Đặc tả đường ống (Tunnel) trong Kitwork: cầu nối tự động giữa giao diện và server (có trên engine2), và thư viện `"kitwork/tunnel"` nối trực tiếp giữa các runtime Kitwork với nhau (chưa có).

## 1. Hai đoạn của Đường ống

Kitwork sử dụng khái niệm **đường ống (tunnel)** cho mọi kênh giao tiếp có kiểm tra định danh và quyền hạn giữa hai phía:

| Đoạn | Hai đầu kết nối | Người viết `.work` thấy gì? | Đường truyền vật lý |
|---|---|---|---|
| **1. Giao diện ↔ Server (Bridge)** | Trình duyệt / WebView ↔ Engine | **Trong suốt:** viết `router.view` / `component.define`, trả về `context.bind({ ... })` hoặc `context.sync({ ... }).bind({ ... })` (xem [`view.md`](view.md) và [`component.md`](component.md)) | Web: `POST <URL trang>?kitwork-call=<việc>` (phương thức khác là `405`) trả về bản vá dữ liệu theo instance `{ seq, page: { ... }, c...: { ... } }` + kênh `GET /_kitwork/changes?tab=...`; App: chưa có trên engine2 |
| **2. Runtime ↔ Runtime** (chưa có) | Hai ứng dụng Kitwork (Desktop ↔ Desktop, Desktop ↔ Mobile, Server ↔ Server) | **Tường minh:** dùng thư viện `"kitwork/tunnel"` và thư mục cửa `.tunnel/` (thiết kế dưới đây) | TLS 1.3 hai chiều (mTLS) trên TCP với khoá Ed25519 và ALPN `kitwork-tunnel/1` |

### Tóm tắt Cầu nối Giao diện ↔ Server (Đoạn 1)

- **Trang là một instance component (`page`), mỗi component con là một instance (`c<hash>`):**
  - Lời gọi việc gửi về chính URL của trang với query `?kitwork-call=<tên_việc>` (kèm `tab`, `seq`, `at`, `args` và các biến `var` của instance). Mỗi tab gửi tuần tự từng lời gọi một (`seq`).
  - Server giữ một **bản ghi theo từng tab (`tab`, id ngẫu nhiên)** trong bộ nhớ của tiến trình, cho trang có tên của `sync`: mã băm từng tên theo instance, và các khoá `kitwork/storage` lượt chạy của tab đã đọc. Ghi một khoá ấy thì kênh của tab báo đổi, tab hỏi lại trang bằng `POST <URL trang>?kitwork-refresh=1`. Ghi `kitwork/database` chưa báo tab.
  - Câu trả lời không bao giờ gửi lại cả trang HTML mà chỉ trả về các tên đã đổi, mỗi tên nguyên giá trị, theo từng instance (`{ seq, page: { left: 2, todos: [ ... ] }, c3fq8w: { ... } }`), và chỉ chứa các tên được liệt kê trên dòng `return` (`context.sync({ ... }).bind({ ... })` hoặc `context.bind({ ... })`).

---

> **Trạng thái (16/11/2026): đoạn 2 chưa có trên engine2; mục 2–5 là thiết kế.** `import * as tunnel from "kitwork/tunnel"`
> là lỗi boot (`kitwork/tunnel is not served by engine2 yet`); `app.tunnel(…)` là lỗi đọc manifest ở dòng ấy
> (`app has no word tunnel`); engine2 không đọc `.tunnel/`.

## 2. Mở cổng ở Host (`app.tunnel`) và Định danh khoá

Trong `application.work`, host khai báo cổng lắng nghe cho đường ống (chưa có):

```js
// application.work
import * as app from "kitwork/application";

app.root("apps");
app.website({ hostname: "shop.com", port: 8080 });
app.tunnel({ port: 4433, hostname: "shop.com" }); // chưa có: hôm nay là lỗi đọc manifest
app.run();
```

- Mỗi ứng dụng tự động có một cặp khoá **Ed25519** lưu trong `.database/tunnel/` (không bao giờ phục vụ ra ngoài).
- **Định danh (`id`)** của ứng dụng là mã băm SHA-256 của khoá công khai (base32 chữ thường). Địa chỉ đường ống có dạng:
  ```js
  tunnel.address(); // "tunnel://5x2k...q7@shop.com:4433"
  ```
- Định danh được xác minh ngay trong lúc bắt tay TLS 1.3, **trước khi bất kỳ máy ảo (VM) nào chạy**.

---

## 3. Ghép đôi một lần (`invite` / `accept`) và Danh sách tin cậy

Hai máy chỉ nói chuyện được với nhau sau khi đã ghép đôi qua một đường dẫn mời dùng một lần, sống ngắn (mặc định `10m`, tối đa `1h`):

```js
import * as tunnel from "kitwork/tunnel";

// Máy A (trong một route có guard quản trị): tạo lời mời
const invitation = tunnel.invite({ expire: "10m" });
// → { link: "tunnel://5x2k...q7@shop.com:4433#gk3...", expires: "..." }

// Máy B: nhận lời mời
const paired = tunnel.accept(link);
// → { ok: true, peer: { id, address } }

// Quản lý danh sách máy đã ghép đôi
const list = tunnel.peers();          // [{ id, address, connected, pairedAt, seenAt }]
tunnel.peer(peerId).delete();         // Gỡ quyền tin cậy và đóng ngay kết nối với máy đó
```

---

## 4. Gửi tin (`tunnel.peer(...).send(...)`)

Tin nhắn đi qua đường ống dùng chung phong bì `{ event, id, data }` với `kitwork/storage` và `context.sse`:

```js
import * as tunnel from "kitwork/tunnel";

const sent = tunnel
  .peer(peerIdOrAddress)
  .timeout("5s")
  .send({
    event: "chat",
    id: "msg-" + Date.now(),
    data: { text: "Xin chào" },
  });

if (!sent.ok) {
  // sent.code: "DENIED" | "NOT_FOUND" | "UNAVAILABLE" | "NETWORK" | "TIMEOUT" | "TOO_LARGE"
  return context.status(502).json({ code: sent.code, error: sent.error });
}
```

---

## 5. Nhận tin tại thư mục cửa `.tunnel/`

Mọi tin từ bên ngoài đi vào ứng dụng phải đi qua thư mục **`.tunnel/`** ở gốc ứng dụng. Tên `export const` chính là tên sự kiện (`event`) mà cửa đó đón nhận:

```js
// app/.tunnel/chat.work
import * as tunnel from "kitwork/tunnel";
import { memory } from "kitwork/storage";

export const chat = tunnel.observe((context) => {
  // context.message: { event: "chat", id: "msg-...", data: { text: "Xin chào" } }
  // context.peer: handle của máy vừa gửi (đã kiểm tra chữ ký khoá Ed25519)
  memory("chat").send(context.message);
  context.peer.send({ event: "seen", id: context.message.id, data: {} });
});
```

- **Không có thực thi từ xa tự động:** Đường ống chỉ chở dữ liệu JSON tới đúng cửa `export const <event> = tunnel.observe(...)` mà ứng dụng đã viết; sự kiện không có cửa đón nhận sẽ bị bỏ qua.
- Nếu `app.tunnel` không bật `lan: true` trên máy chủ, mọi kết nối đường ống gọi tới địa chỉ IP nội bộ đều bị chặn (`code: "DENIED"`) giống luật chống SSRF của `kitwork/http`.


# SSE

Đặc tả luồng sự kiện từ server xuống trình duyệt (Server-Sent Events) qua `context.sse` và `kitwork/storage`.

## 1. Tổng quan

Trong `.work`, một route `router.get` mở luồng sự kiện bằng `context.sse`; tin nhắn có phong bì thống nhất `{ event, id, data }`, được đẩy vào từ bất kỳ đâu qua `.send(...)` của `kitwork/storage` và được luồng lắng nghe qua `.observe(...)` (hoặc `.subscribe(...)`):

```js
// shop.com/events/router.work
import * as router from "kitwork/router";
import { memory } from "kitwork/storage";
import { currentUser } from "app/core/session.work";

// GET /events: mở luồng SSE của người đang đăng nhập
router.get((context) => {
  const user = currentUser(context);
  const stream = context.sse.reconnect("3s");
  memory("events").segment(user.id).observe((message) => {
    stream.send(message);
  });
  return stream;
});

// POST /events: gửi một tin vào luồng
router.post((context) => {
  const user = currentUser(context);
  const { text } = context.request.json();
  if (typeof text !== "string" || text.length > 2000) {
    return context.status(400).json({ message: "Tin nhắn không hợp lệ" });
  }
  memory("events")
    .segment(user.id)
    .send({ event: "chat", id: "msg-" + Date.now(), data: { text } });
  return context.json({ ok: true });
});
```

---

## 2. Mở luồng (`context.sse`)

- **Chỉ dùng trong `router.get`.** Truy cập `context.sse` trong guard hay phương thức khác (`post`…) là lỗi lúc chạy, ở request chạm tới; trong hàm của `router.view` cũng vậy (`router.view` vẽ trang của nó; luồng là `router.get` ở một thư mục không có `router.view`).
- **Handler phải trả về chính `stream`:** Chạm vào `context.sse` là chọn trả lời bằng luồng sự kiện (`Content-Type: text/event-stream`, `Cache-Control: no-cache`, không nén). Các chỉnh sửa `.header(...)`, `.headers(...)`, `.cookie(...)` viết trước `context.sse` được giữ nguyên.
- **Không đi kèm `.cache()` hay `.persist()`:** Route mở luồng sự kiện trực tiếp không được phép cache (`cache`, `persist` chưa phục vụ: bỏ qua kèm một dòng boot, nên chưa có lỗi riêng). `.guard()` và `.limit()` hoạt động bình thường lúc kết nối.
- **`context.sse.reconnect("3s")`:** Ghi trường `retry: 3000` ở đầu luồng để trình duyệt tự kết nối lại sau khoảng thời gian đó khi đứt mạng.

---

## 3. Gửi tin xuống luồng (`stream.send`)

Hai cách viết tương đương nhau cho cùng một phong bì `{ event, id, data }`:

```js
stream.send({ event: "chat", id: "msg-123", data: { text: "Xin chào" } });

// hoặc nối chuỗi:
stream.event("chat").id("msg-123").data({ text: "Xin chào" });
```

- **`data` là bắt buộc** và luôn được mã hoá thành JSON (kể cả khi `data` là chuỗi).
- **`event` và `id` là chuỗi tuỳ chọn,** không chứa ký tự xuống dòng. Nếu không đặt `event`, trình duyệt nhận sự kiện mặc định `"message"`. Sau mỗi lần gửi bằng `.data(...)`, `event` và `id` tự động trở về trống cho tin tiếp theo.
- Tin gọi `stream.send(...)` trước lệnh `return stream` được giữ trong bộ đệm mở đầu (tối đa 10.000 tin hoặc 4 MiB) và đẩy đi ngay khi luồng mở — rất tiện để gửi bù các tin bị lỡ dựa theo header `Last-Event-ID`.
- **`stream.close()`** đóng luồng ngay lập tức; **`stream.closed`** cho biết luồng đã đóng hay chưa.

---

## 4. Lắng nghe nguồn tin: `observe` và `subscribe`

```js
// Dạng đầy đủ (khi cần lọc hoặc biến đổi tin trước khi gửi):
router.get((context) => {
  const stream = context.sse.reconnect("3s");
  memory("events").segment(currentUser(context).id).observe((message) => {
    stream.send(message);
  });
  return stream;
});

// Dạng rút gọn (chuyển tiếp nguyên vẹn mọi tin từ một hoặc nhiều nguồn):
router.get((context) =>
  context.sse
    .reconnect("3s")
    .subscribe(memory("events").segment(currentUser(context).id))
);
```

- **Tự động dọn dẹp:** Mọi observer đăng ký trong request mở luồng sẽ tự động đóng ngay khi kết nối SSE đóng (do trình duyệt tắt tab, `stream.close()`, lỗi mạng hoặc tắt máy chủ). Nạp lại bản mới (hot reload) giữ luồng đang mở. Người viết không phải tự huỷ đăng ký.
- **Giới hạn và an toàn:**
  - Luôn phân đoạn (`segment`) khoá của nguồn tin theo danh tính phiên đã kiểm tra (`currentUser(context).id`), không lấy thẳng từ `context.query` để tránh lộ luồng của người dùng khác (IDOR).
  - Engine gửi nhịp giữ kết nối (comment line) mỗi 15 giây và áp hạn ghi 15 giây để tự động thu hồi các kết nối bị treo.
  - Hàng chờ của mỗi observer có trần (256 tin); nếu trình duyệt đọc quá chậm làm đầy hàng chờ, luồng đó tự đóng mà không làm chậm người gửi.

---

## Trạng thái triển khai (16/11/2026)

engine2 làm các mục trên (`library/router/stream.go`, `library/storage/hub.go`, `host/stream.go`), với các điểm sau.

- **Mở luồng.** `context.sse` đọc không gọi (cả `context.response.sse`, `context.header(…).sse`); đọc lại là cùng một
  luồng. Chỉ trong cửa GET; HEAD trả header của luồng và không giữ gì. Trong guard, trong `router.view`, trong method khác, sau `status` khác
  200, sau `type`, sau chữ của trang (`title`…), sau một câu trả lời khác là lỗi; chỉnh sửa hay câu trả lời viết sau
  `context.sse` cũng là lỗi. Handler đọc `context.sse` mà trả thứ khác luồng là 500, lý do vào log. Các lỗi này báo lúc
  chạy, ở request chạm tới. engine2 chưa phục vụ
  `.cache()`, `.persist()` (bỏ qua kèm một dòng boot), nên chưa có lỗi riêng cho route mở luồng có cache.
- **Tin.** `data` bắt buộc, luôn viết thành JSON (chuỗi có ngoặc kép); `data: undefined` là lỗi (JSON không viết được),
  `data: null` được. `event` rỗng không ghi dòng `event:` (trình duyệt nhận `message`); `id` ghi khi có, kể cả rỗng. Dạng nối
  `.event(…).id(…).data(…)`: `event` hay `id` viết hai lần cho một tin là lỗi. `reconnect(span)` nhận thời lượng viết kiểu Go,
  từ 1ms tới 24h; gọi khi luồng đã mở (trong hàm của `observe`) thì ghi `retry:` ngay. `subscribe` nhận đúng một handle;
  hai nguồn là hai lần `subscribe`. Tin gửi trước `return stream` tối đa 10.000 tin và 4 MiB, quá là `INVALID_ARGUMENT`.
  Luồng đã đóng thì `send` không làm gì và `closed` là `true`.
- **`observe`.** Trả observer có `close()`. Chạy được trong handler và guard của request; trong routine, job, lịch là
  `UNSUPPORTED`. Request không trả lời bằng luồng thì observer đóng khi request xong, hàm không chạy lần nào. `send` tới mọi
  observer của đúng khoá (nơi lưu, tên, segment) trong tiến trình; không ai nghe thì tin mất. Mỗi app tối đa 10.000 observer
  (`TOO_LARGE`); app đã đóng thì `observe` là `UNAVAILABLE`.
- **Chạy hàm.** Hàm của `observe` chạy trên VM của request, từng tin một, mỗi lần với năng lượng và thời gian của một request
  (`Host.RequestTimeout`), không chung với handler. Hàm hỏng thì luồng đóng, log ghi
  `GET /path (/path/router.work, observe): the stream closed: …`, trình duyệt không nhận gì về lỗi.
- **Giữ luồng.** Header `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `X-Accel-Buffering: no`, cùng các
  header handler đặt trước; không nén. Nhịp `: heartbeat` mỗi 15 giây. Mỗi lần ghi có hạn 15 giây, ghi xong thì xoá hạn
  (HTTP/2 coi hạn là đồng hồ đặt lại luồng dù đang rỗi); trình duyệt không đọc thì lần ghi hỏng, net/http huỷ request, hàm
  đang chạy dừng và luồng đóng. Hàng chờ mỗi observer 256 tin; đầy thì luồng đóng, log ghi một dòng; người gửi không đợi.
- **Trần và đóng.** Mỗi app tối đa 1.000 luồng mở, mọi site của app chung trần; quá là 503 kèm `Retry-After: 5`. Host đóng
  thì mọi luồng đóng, kể cả lần ghi đang chờ, và luồng mới là 503. Ba con số đặt được qua `Host.StreamLimit`,
  `Host.StreamHeartbeat`, `Host.StreamWriteTimeout`.
- **Chưa có.** Lỗi boot cho `context.sse` ngoài `router.get`; đóng luồng khi phiên hết hạn (engine2 chưa có phiên); cảnh báo
  khoá của `observe` dựng từ `context.query`, `request.json`, `request.form` (IDOR); cảnh báo khi host chạy nhiều máy; đóng
  luồng khi nạp lại bản mới (nạp lại giữ storage và luồng đang mở của app, `host/reload.go`).

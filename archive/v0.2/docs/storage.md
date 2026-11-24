# Storage

Đặc tả lưu trữ khoá–giá trị (`memory`, `disk`, `datastore`) và phát/nhận sự kiện (`send`/`observe`) qua thư viện `"kitwork/storage"`.

## 1. Ba tầng lưu trữ, một từ vựng duy nhất

Thư viện `"kitwork/storage"` cung cấp ba nơi lưu trữ với cùng một bộ phương thức:

```js
import { memory, disk, datastore } from "kitwork/storage";
```

| Hàm tạo | Nơi lưu | Sống qua khi khởi động lại? | Dùng cho |
|---|---|---|---|
| `memory(name)` | RAM của tiến trình (theo từng app) | Không | Đếm nhanh, giữ trạng thái tạm, và làm nguồn phát sự kiện `send`/`observe` cho SSE |
| `disk(name)` | Đĩa cục bộ (`.database/disk/`) | Có | Bộ nhớ đệm bền vững trên máy (kết quả tính toán nặng, dữ liệu tra cứu) |
| `datastore(name)` | Cơ sở dữ liệu (`.database/storage/kitwork.db`) | Có | Khoá–giá trị bền vững qua người ghi KitDB của tiến trình (một tiến trình giữ file; các site của app dùng chung) |

---

## 2. Phân đoạn khoá (`segment`) và Thời hạn (`expire`)

Mô tả đứng trước, hành động đứng cuối:

```js
// Phân đoạn khoá theo từng phần (không tự nối chuỗi bằng dấu ":" để tránh va chạm khoá)
const cartStore = datastore("carts").segment(user.id);

// Ghi với thời hạn tự hết hạn
cartStore.expire("7d").set({ items: [{ id: 1, qty: 2 }] });

// Đọc giá trị (trả về null nếu chưa có hoặc đã hết hạn)
const cart = cartStore.get();

// Đọc kèm hàm tính toán (Read-through cache: nếu chưa có thì chạy hàm, lưu lại với expire rồi trả về)
const stats = disk("dashboard")
  .segment(shop.id)
  .expire("10m")
  .get(() => computeHeavyStats(shop.id));

// Xoá một khoá hoặc xoá cả nhánh phân đoạn
cartStore.delete();
datastore("carts").clear();
```

### Luật của `segment` và `expire`

- **`.segment(...parts)`:** Nhận chuỗi hoặc số nguyên. Có thể gọi nối tiếp `.segment("shop", shopId).segment("user", userId)`. Mọi khoá đều tự động được cô lập theo ứng dụng (`app/` hoặc `apps/<identity>/`).
- **`.expire(duration)`:** Nhận chuỗi thời lượng (`"30s"`, `"10m"`, `"24h"`, `"7d"`). Đứng trước `.set(value)` hoặc `.get(fn)`.
- **Giá trị lưu trữ:** Phải là giá trị mã hoá được sang JSON (`null` không phải là giá trị lưu — muốn xoá khoá thì gọi `.delete()`).

---

## 3. Phát và Lắng nghe sự kiện (`send` và `observe`)

Cùng một khoá đã phân đoạn trên `memory(name)` (hoặc `datastore(name)`) còn là kênh phát/nhận tin nhắn theo phong bì `{ event, id, data }`, kết nối trực tiếp với Server-Sent Events (`context.sse` — xem [`sse.md`](sse.md)):

```js
// Nơi phát tin (trong router.post, queue job, schedule, hoặc hook after của database):
memory("notifications")
  .segment(user.id)
  .send({
    event: "order-updated",
    id: "evt-" + order.id,
    data: { orderId: order.id, status: order.status },
  });

// Nơi lắng nghe (trong router.get mở luồng SSE):
router.get((context) =>
  context.sse
    .reconnect("3s")
    .subscribe(memory("notifications").segment(currentUser(context).id))
);
```

- **`send({ event?, id?, data })`:** `data` là bắt buộc; `event` và `id` là chuỗi tuỳ chọn. Gửi khi không có ai đang `observe` là thao tác rỗng (không lỗi, không giữ tin trong RAM).
- **`observe((message) => { ... })`:** Đăng ký hàm nhận từng phong bì `{ event, id, data }` gửi vào đúng khoá phân đoạn đó, trong tiến trình này. Chỉ chạy trong route trả luồng `context.sse`; observer tự đóng khi luồng đóng, `close()` đóng sớm. Gọi trong job, schedule, routine là lỗi.


---

## Trạng thái triển khai (07/11/2026, `send` và `observe` 08/11)

engine2 làm các mục trên (`library/storage`). `send` và `observe` làm từ 08/11, cùng `context.sse`: chi tiết ở "Trạng thái
triển khai" của [`sse.md`](sse.md). `set(null)`, `set(undefined)` là lỗi nói dùng `delete()`; `get(fn)` mà
hàm trả `null` không lưu gì; `clear()` xoá khoá và mọi khoá dưới nó, trả số giá trị còn sống đã xoá; `segment` nhận chuỗi
không rỗng hay số nguyên. Engine có thêm (chưa ghi ở trên): `exists()`, `ttl()` (mili giây còn lại), `name()`, `key()`,
`keys()`; `expire` nhận cả số mili giây và mốc lịch (`"nextday 03:00"`, `"weekly fri 18:00"`) theo `module.timezone` của
app; trần của mỗi app (giá trị 1 MiB, khoá 4096 byte; `memory` 10.000 khoá, 64 MiB, quá thì đẩy khoá ít dùng nhất ra;
`disk` 100.000 khoá, 1 GiB, `datastore` 100.000 khoá, 256 MiB, quá thì lần ghi lỗi). Khoá, giá trị,
file giữ định dạng của engine đầu, nên dữ liệu nó đã lưu đọc được.
- 08/11/2026 (lát 4 của `view.md`): một handle gọi trong lần chạy vẽ trang ghi lại khoá nó đọc (`get`, `exists`, `ttl`); `set`,
  `delete` có xoá, `clear` có dọn báo app khoá (hay nhánh) đã đổi, để tab đã đọc nó hỏi lại trang (`host/tab.go`).

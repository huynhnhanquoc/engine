# Queue

Hàng đợi công việc bền vững qua thư viện `"kitwork/queue"`, như engine2 làm hôm nay (16/11/2026: file
này tả engine; lý do và lịch sử ở `docs/KITWORK_QUEUE.md` của repo gốc).

## 1. Tổng quan

Công việc cần đảm bảo thực thi bền vững (gửi email, xuất báo cáo, đồng bộ đối tác, xử lý sau khi ghi database) được khai báo ở cấp file của một file `.work` của ứng dụng. Từ 17/11/2026 (lát 2 của `docs/KITWORK_MODULE_WORK.md` ở repo gốc) bảng, job, lịch và luật chung của hàng đợi, của lịch tự khai ở cấp file của **bất kỳ file `.work` thường nào** của app (không phải `module.work`, `router.work`, `config.work`, `design.work`, `components.work`): boot đọc mọi file như vậy, ở mọi thư mục, trừ thư mục gạch dưới (bản nháp) và thư mục chấm (`.database/`…); `.shared/` và `.schedule/` ở gốc app, chỗ của code tới 16/11/2026, là lỗi boot nêu file đầu tiên và chỗ mới (`.shared/components/` vẫn là của `component.define`); `config.work` viết luật chung của hàng đợi, của lịch cũng là lỗi boot, vì file có vai không được đọc cho việc nền. Ví dụ dưới gom code vào `core/`, tên do app chọn; engine không gán nghĩa cho tên thư mục nào.

```js
// app/core/camOn.work
import * as queue from "kitwork/queue";
import * as http from "kitwork/http";
import * as file from "kitwork/file";

const env = file.env("app/.env");

export const camOn = queue.job((context) => {
  const { email, orderId } = context.data();
  const res = http
    .post("https://api.mail.example.com/send")
    .bearer(env.MAIL_API_KEY)
    .json({ to: email, subject: "Xác nhận đơn #" + orderId })
    .send();
  if (!res.ok) {
    return error("Không gửi được email: " + res.error);
  }
}).retry(3).timeout("30s");
```

```js
// Gửi việc vào hàng đợi từ router.work:
import { camOn } from "app/core/camOn.work";

camOn.send({ email: "khach@example.com", orderId: 1024 });
```

---

## 2. Khai báo Job

- **Khai ở cấp file của một file `.work` thường:** `queue.job(...)` trong `router.work`, `config.work`, `design.work`,
  `module.work` là lỗi lúc boot; gọi trong một hàm lúc request là lỗi lúc chạy.
- **Tên `export const` là tên định danh của Job:** `export const camOn = ...` đăng ký job tên `"camOn"`. Tên phải duy nhất trong app; hai file cùng tên là lỗi boot nêu cả hai.
- **Luật nối sau `.job(fn)`, mỗi luật một lần:**
  - `.retry(n)`: số lần thử thêm khi job lỗi, số nguyên 0–100, mặc định 0 (`.retry(3)` là bốn lần tổng cộng); backoff
    1 giây, 2 giây… tối đa 30 phút ±20%.
  - `.timeout(hạn)`: thời gian chạy tối đa cho mỗi lần thử, `"1ms"`–`"24h"`; không viết thì không có hạn.
- **`context` của job:** `context.data()` là dữ liệu đã gửi; `context.job` mang `id`, `name`, `attempt`, `maxAttempts`,
  `isFinalAttempt`, `createdAt`, `startedAt`, `log(…)`.
- **Báo lỗi bằng builtin chung:** `return error(message)` làm lần thử thất bại để queue áp dụng chính sách retry. Không có `queue.fail`; nếu dùng `safe` rồi xử lý hết lỗi và trả thành công, queue coi lần thử đó thành công (xem [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe)).
- **Chữ đã bỏ** là lỗi nêu chữ mới: `queue.handle`, `queue.dispatch`, `queue.send("tên", …)`, `queue.jobs()`,
  `queue.failed()`, `queue.resend`, `queue.retry(id)`.
- **Cấu hình chung của hàng đợi:** ở cấp file của một file `.work` thường, mỗi chữ một lần trong app (hai chỗ là lỗi boot
  nêu cả hai; cùng lời gọi chạy lại qua import thì không tính). Tới 16/11/2026 chỉ trong `.shared/queue/config.work`.
  ```js
  // app/core/rules.work
  import * as queue from "kitwork/queue";
  import { jobs } from "app/core/jobs.work";

  queue.parallel(4);     // Số job chạy đồng thời tối đa của ứng dụng (1–100, mặc định 4)
  queue.keep("30d");     // Thời gian giữ lịch sử các lần chạy đã xong (từ "1h", mặc định 30 ngày)
  queue.database(jobs);  // Database giữ các lần chạy (mục "Database của các lần chạy")
  ```

---

## 3. Đưa việc vào hàng đợi (`send`)

```js
// Gửi chạy ngay
const run = camOn.send({ email: user.email, orderId: order.id });

// Gửi hẹn chạy sau một khoảng thời gian và chống trùng bằng key
const delayed = camOn.send({ orderId: order.id }, { delay: "10m", key: "remind-order-" + order.id });
```

- **Dữ liệu** là một giá trị JSON bất kỳ (không viết thì là `null`), tối đa 64 KiB và 64 tầng lồng; hàm, struct, bytes,
  NaN bị từ chối và nói chỗ.
- **`delay`:** Hoãn thời điểm bắt đầu chạy (`"30s"`, `"10m"`, `"2h"`).
- **`key`:** Chống trùng: `key` giữ chỗ tới khi bản ghi của lần chạy bị dọn theo `keep`, ở mọi trạng thái; gửi trùng trả
  lần chạy đang giữ key kèm `duplicate: true`, không tạo bản ghi thứ hai.
- `send` trả `{ id, name, status: "pending" }` sau khi lần chạy đã lưu. Gửi chưa đi cùng giao dịch với một lần ghi
  database: lần chạy được lưu bằng một lần ghi của riêng nó.

---

## 4. Theo dõi và Điều khiển (`list`, `runs`, `cancel`, `replay`)

Thư viện `"kitwork/queue"` và từng job handle cung cấp các phương thức quản trị để dựng trang theo dõi nội bộ:

```js
import * as queue from "kitwork/queue";
import { camOn } from "app/core/camOn.work";

const allJobs = queue.list();                      // các job đã khai: name, retry, timeout, source
const failedRuns = camOn.status("failed").runs();  // lần chạy theo trạng thái
const latest = queue.runs(10);                      // không gì (50), một số, hay { page, limit } (tối đa 1000)

camOn.cancel(runId);
camOn.replay(runId);
```

- **Trạng thái của một lần chạy:** `pending`, `running`, `completed`, `failed`, `cancelling`, `cancelled`.
- **`cancel(id)`:** `pending` thành `cancelled`; `running` thành `cancelling` và lần thử đang chạy bị dừng; lần đã
  `cancelled` hay `cancelling` trả nguyên như vậy; lần đã xong là `CONFLICT`.
- **`replay(id)`:** lần chạy đã xong (`failed`, `cancelled`, `completed`) thành một lần chạy mới có `replayOf`; lần chưa xong
  là `CONFLICT`; job không còn khai là `NOT_FOUND`.
- id của job khác đưa qua handle là `NOT_FOUND`.

## 5. Chi tiết engine2

`library/queue`, `host/background.go`.

- **Khai:** host chạy mọi file `.work` thường của app một lần lúc app boot (file viết luật chung của hàng đợi, của lịch
  trước, rồi theo đường dẫn), sau lượt khai bảng và `config.work` của gốc app, trước các site. Tên job là tên `export const`: handle biết tên mình theo chỗ
  viết `queue.job(` (dấu chấm, `file:dòng:cột` như runtime nói), host đọc chỗ đó từ chính file. Lỗi boot (app đóng, site trả
  503) kèm chỗ: job không export, `export default`, `export { a as b }`, một khai báo hai tên, hai job cùng tên, `queue.job`
  trong file có vai (`router.work`, `config.work`, `design.work`, `module.work`) hay lúc request, hàm không phải hàm của chương trình,
  `.retry`/`.timeout` nối hai lần, sai giá trị hay nối lúc request, `send`/`cancel`/`replay` lúc file được đọc,
  `.shared/queue/` trong một site (thư mục chấm của site không được đọc). Hàng đợi con (thư mục với luật riêng) chưa có.
- **Gửi:** `send(data, { delay, key })` lưu lần chạy vào `kitwork_runs` của `.database/application/kitwork.db` (qua người ghi
  KitDB duy nhất của file, như bảng của app; bảng y hệt engine cũ, nên lần chạy engine cũ để lại đọc được) rồi mới trả
  `{ id, name, status: "pending" }`. Trần dữ liệu 64 KiB (`TOO_LARGE`).
- **Worker:** chạy khi host phục vụ (`Serve` gọi `StartBackground()`; một lần kiểm, chỉ boot, không chạy gì). App khai ít
  nhất một job thì có worker của queue (app không có site cũng vậy); không khai job nào thì không có. Lease 30 giây,
  heartbeat mỗi 10 giây, chỉ chủ token ghi kết quả (fencing); `.timeout` mỗi lần thử, hết giờ ghi `TIMEOUT` ngay;
  `return error(…)` hay lỗi không bắt là thất bại; job không còn khai thì lần chạy của nó `failed` `NOT_FOUND`; lease hết hạn
  của tiến trình trước thì lần chạy về lại hàng. Dọn theo `keep` mỗi phút. Đóng host (`Close`) thì worker ngừng nhận và chờ
  các lần đang chạy tới 30 giây; quá thì in một dòng, lần chạy về lại hàng khi hết lease. App không boot thì không chạy gì.
- **Database của các lần chạy:** `queue.database(db)` nhận một database của app đã import (`connection(…).tables(…)`).
  `connection()` là database của app, mặc định nói lại; `connection("tên")` (một tên SQLite của `app.databases`) đặt các lần
  chạy và định nghĩa job vào file đó, bảng tạo lần đầu dùng, qua người ghi duy nhất của file. Giá trị khác là lỗi boot. Mỗi
  file của danh mục giữ các lần chạy của một app (`kitwork_runs` không ghi app nào): app thứ hai gọi cùng file thì boot in
  một dòng nêu app đang giữ, và các lần chạy của nó ở lại database của nó. Đổi database không dời lần chạy: lần chạy chưa xong
  còn ở database của app thì boot in số lần đó. Dòng của worker nêu nơi các lần chạy nằm.
- **Chưa có:** `where(fn)` là `UNSUPPORTED`; gửi cùng giao dịch với một lần ghi; hàng đợi con; `queue.database(db)` tới
  Postgres. KitDB ghi lịch sử mọi bảng `kitwork_*`, nên mỗi lần gửi, nhận, gia hạn, ghi kết quả là một commit của lịch sử
  file.

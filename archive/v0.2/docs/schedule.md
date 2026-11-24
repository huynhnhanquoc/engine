# Schedule

Lập lịch công việc định kỳ và hẹn giờ qua thư viện `"kitwork/schedule"`, như engine2 làm hôm nay (16/11/2026:
file này tả engine; lý do và lịch sử ở `docs/KITWORK_SCHEDULE.md` của repo gốc).

## 1. Tổng quan

Các công việc chạy tự động theo thời gian được khai ở cấp file của một file `.work` của ứng dụng (xem [`queue.md`](queue.md) mục 1: tự khai từ 17/11/2026, tới đó chỉ trong `.schedule/`). Thay vì biểu thức cron 5 dấu sao khó đọc, Kitwork dùng các phương thức mô tả thời gian rõ nghĩa:

```js
// app/core/baoCao.work
import * as schedule from "kitwork/schedule";
import { db } from "app/core/shop.work";
import { camOn } from "app/core/camOn.work";

// Chạy mỗi ngày lúc 08:00 sáng (theo module.timezone của ứng dụng)
export const baoCaoSang = schedule.daily("08:00").run((context) => {
  const delivered = db.count((orders) => orders.status === "giao");
  camOn.send({ report: "daily", total: delivered });
});

// Chạy mỗi 15 phút một lần
export const donDepPhien = schedule
  .every("15m")
  .timeout("2m")
  .run(() => {
    const now = Date.now();
    db.delete((sessions) => sessions.expires < now);
  });

// Chạy đúng một lần vào thời điểm chỉ định
export const moDotKhuyenMai = schedule
  .on("2026-12-01T00:00:00+07:00")
  .once(() => {
    // ...
  });
```

---

## 2. Từ vựng đặt lịch (`kitwork/schedule`)

### 2.1. Chọn nhịp thời gian (đứng đầu chuỗi)

| Lời gọi | Ví dụ | Ý nghĩa |
|---|---|---|
| `schedule.every(khoảng)` | `schedule.every("5m")`, `schedule.every("1d")` | Lặp sau mỗi khoảng, tối thiểu `"1s"`; từ một ngày trở lên là số ngày tròn (`"1d"`, `"2d"`, `"1w"`), không có tháng hay năm; đếm từ nửa đêm của múi giờ lịch |
| `schedule.hourly(n)` | `schedule.hourly(2)` | Mỗi `n` giờ, đúng phút `:00` |
| `schedule.daily(giờ, …)` | `schedule.daily("09:00", "17:00")` | Hằng ngày vào các giờ `"HH:MM"` |
| `schedule.monday(giờ, …)` … `sunday` | `schedule.monday("08:30")` | Thứ ấy hằng tuần |
| `schedule.weekly(thứ, …).at(giờ, …)` | `schedule.weekly("monday", "friday").at("08:30")` | Các thứ trong tuần; giờ đi qua `.at(…)` |
| `schedule.monthly(ngày, …).at(giờ, …)` | `schedule.monthly(1, 15).at("00:00")` | Các ngày 1–31; tháng không có ngày ấy thì bỏ qua |
| `schedule.doubleday(giờ)` | `schedule.doubleday("09:00")` | Ngày trùng tháng: 1/1, 2/2 … 12/12 |
| `schedule.firstday(giờ)`, `lastday(giờ)` | `schedule.lastday("09:00")` | Ngày đầu, ngày cuối tháng |
| `schedule.on(thời điểm)` | `schedule.on("2026-12-31T23:59:00+07:00")` | Một thời điểm viết đủ ngày, giờ, múi giờ; chỉ đi với `.once(fn)` |

- **Bổ nghĩa:** `.first("month")`, `.last("month")`, `.before(n)`, `.after(n)` (như `lastday("09:00").before(3)`),
  `.timezone("Asia/Tokyo")` của riêng lịch.
- **Múi giờ:** giờ `"HH:MM"` tính theo `.timezone(…)` của lịch, không có thì theo `module.timezone(…)` trong `module.work` của app,
  không có nữa thì theo `app.timezone(…)` của host trong `application.work` (17/11/2026); không tầng nào khai thì đếm theo
  UTC và boot in một dòng cho mỗi lịch, nêu cả ba chỗ. `on(…)` giữ độ lệch viết trong thời điểm của nó.
- Giờ viết dính vào thứ (`weekly("monday 08:30")`) hay làm đối số của `monthly` (`monthly(1, "00:00")`) là lỗi chỉ sang
  `.at(…)`; `weekly("monday", "08:30")` là lỗi "thứ "08:30" không có".

### 2.2. Chỉnh sửa và Kết thúc khai báo

- **Chỉnh sửa (đứng trước `.run` / `.once`):**
  - `.timeout(hạn)`: thời gian chạy tối đa của một lần, `"1ms"`–`"24h"`; không viết thì không có hạn; dưới 1ms là lỗi.
  - `.retry(n)`: số lần thử lại thêm khi lần chạy đó bị lỗi (mặc định `0`).
- **Hành động đăng ký (đứng cuối):**
  - `.run(fn)`: lịch lặp lại.
  - `.once(fn)`: chạy đúng một lần, vào lần tới giờ đầu tiên; đi sau mọi quy tắc. `on(…)` chỉ đi với `.once`.
- `context.job` mang `at` (giờ lẽ ra chạy); `context.data()` là lỗi (lịch không có dữ liệu gửi).

### 2.3. Luật chung của mọi lịch

Ở cấp file của một file `.work` thường, mỗi chữ một lần trong app (tới 16/11/2026 chỉ trong `.schedule/config.work`):

```js
// app/core/rules.work
import * as schedule from "kitwork/schedule";
import { jobs } from "app/core/jobs.work";

schedule.keep("14d");          // Giữ lịch sử các lần chạy trong 14 ngày
schedule.database(jobs);       // Database giữ các lần chạy, như queue.database(db)
```

`schedule.session({ … })` chưa có: boot đọc rồi in một dòng, lịch chạy không phiên.

---

## 3. Luật thực thi và Điều khiển

1. **Không chạy chồng lấn:** tới giờ khi lần chạy trước của cùng lịch chưa xong thì bỏ lần mới, không ghi lần chạy nào;
   `once()`, `now()` không bao giờ bị bỏ.
2. **Lưu vết bền vững trong `kitwork_runs`:** mỗi lần tới giờ là một lần chạy (kind `schedule`), lưu trước khi chạy, để biết
   lần cuối chạy khi nào, thành công hay lỗi, và không chạy lặp khi khởi động lại tiến trình. Trạng thái: `pending`,
   `running`, `completed`, `failed`.
3. **Kích hoạt tức thì và Tra cứu lịch sử:**
   - `baoCaoSang.now()`: lưu một lần chạy ngay ngoài lịch, trả `{ id, name, status, at }`.
   - `baoCaoSang.runs()`, `schedule.runs()`, `schedule.status("failed").runs(10)`: không gì, một số, hay `{ page, limit }`;
     mỗi lần chạy có `at` và `trigger` (`schedule`, `now`).
   - `schedule.list()`: các lịch của ứng dụng, mỗi lịch `name`, `rule`, `mode`, `retry`, `timeout`, `timezone`, `next`,
     `source`.

## 4. Chi tiết engine2

`library/schedule` (`rule.go`, `chain.go` dời nguyên từ `utilities/schedule` của engine cũ, cùng test của chúng), chạy trên
máy của queue (`library/queue`), `host/background.go`.

- **Khai:** host chạy mọi file `.work` thường của app một lần lúc app boot, cùng lượt với job (file viết luật chung
  trước). Tên lịch là tên
  `export const`, như job. Lỗi boot kèm chỗ: giờ sai, `weekly`/`monthly` thiếu `.at`, `.at` sau `daily`, `first` với `last`,
  `on(…).run`, hai hàm kết thúc, nối sau `.run`, lịch không export, chưa kết thúc, hai lịch cùng tên, `schedule.<lịch>(…)`
  trong file có vai (`router.work`, `config.work`, `design.work`, `module.work`; gọi trong một hàm lúc request là lỗi lúc
  chạy), `.schedule/` trong một site, `schedule.on(…)` đã qua lúc khai (trừ khi
  `kitwork_schedules` giữ nó cùng quy tắc), luật chung gọi ở hai chỗ, sai giá trị, hay viết trong file có vai.
- **Đồng hồ:** app khai ít nhất một lịch thì có đồng hồ của lịch; không có lịch nào thì không. Mỗi lần tới giờ khoá theo giờ
  lẽ ra chạy; `kitwork_schedules` giữ quy tắc đã đồng bộ, giờ `once()` đã chọn, lần tới giờ cuối đã xử lý. Lịch mới hay đổi
  quy tắc (cả múi giờ) đếm từ lúc đồng bộ; lần lỡ khi app tắt chạy bù một lần, lần gần nhất. Thử lại, hết giờ, lease như
  queue.
- **Database:** `schedule.database(db)` như `queue.database(db)`: các lần chạy, định nghĩa và trạng thái của lịch nằm ở file
  đó; mỗi loại theo lời gọi của nó, job lịch gửi theo `queue.database`.
- **Chưa có:** `schedule.session({ … })` (in một dòng); `where(fn)` là `UNSUPPORTED`; `yearly`, `lastworkday`, `.occurrence`
  (lỗi nêu chúng còn là đề xuất).

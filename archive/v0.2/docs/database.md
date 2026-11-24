# Database

Đặc tả kết nối và truy vấn cơ sở dữ liệu (`"kitwork/database"`) trong `.work`. Struct, kiểu và luật của trường (`"kitwork/schema"`) ở [`schema.md`](schema.md); bảng là một struct đặt vào `connection().tables({ … })`.

## 1. Khai báo bảng

Struct (`struct`) và kết nối (`connection`) khai ở cấp file của một file `.work` thường của ứng dụng (không phải
`router.work`, `config.work`, `design.work`, `module.work`); boot đọc mọi file như vậy (từ 17/11/2026; tới đó chỉ
`.shared/schema/`, xem [`queue.md`](queue.md) mục 1):

```js
// app/core/shop.work
import { struct, id, string, choice, integer, decimal, boolean, timestamp, json, foreign } from "kitwork/schema";
import { connection } from "kitwork/database";
import { camOn } from "app/core/camOn.work";

const users = struct({
  id: id(),
  email: string().unique(),
  name: string(),
  role: choice("member", "staff", "admin").default("member"),
  createdAt: timestamp().created(),
});

const orders = struct({
  id: id(),
  userId: foreign(users.id),
  amount: decimal(2),
  status: choice("moi", "giao", "huy").default("moi"),
  meta: json().nullable(),
  createdAt: timestamp().created(),
  updatedAt: timestamp().updated(),
});

const posts = struct({
  id: id("kitwork"),
  title: string().searchable(),
  body: string().searchable(),
  published: boolean().default(false),
});

export const db = connection()
  .before("insert", (users) => ({ ...users, email: users.email.trim().toLowerCase() }))
  .after("insert", (orders) => camOn.send({ orderId: orders.id }))
  .tables({ users, orders, posts });

export const billing = connection()
  .schema("billing")
  .tables({
    invoices: struct({
      id: id(),
      orderId: integer().index(),
      total: decimal(2),
      issuedAt: timestamp().created(),
    }),
  });
```

### Luật khai báo

- **Chỉ trong file thường:** Import `"kitwork/schema"`, gọi `connection()` trong file có vai là lỗi lúc boot. `connect()` chưa có trong engine2: gọi ở đâu cũng là lỗi.
- **Khai một lần:** Mỗi cặp `(connection, schema)` chỉ được khai báo đúng một lần trong app; file khác import handle đã `export` (file import chạy lại khai báo ấy, tìm lại theo chỗ viết).
- **Tên của host:** `connection("tên")` là một tên của `app.databases` mà app được cấp (`apps`); hôm nay chỉ tên `sqlite` chạy, chưa có `.schema(…)` trên tên của host.
- **Quản lý Schema như PostgreSQL:**
  - `connection().tables({ ... })` làm việc trên schema mặc định (`public`, lưu ở `.database/application/kitwork.db`).
  - `connection().schema("billing").tables({ ... })` làm việc trên schema `"billing"` (lưu ở `.database/application/billing.db`).

---

## 2. Kiểu cột, khoá và khoá ngoại: [`schema.md`](schema.md)

Kiểu của trường, luật (`.nullable()`, `.default()`, `.min()`, `.max()`, `.transform()`), khoá (`id(…)`, `.key()`), khoá ngoại
(`foreign(users.id)`, `.cascade()`) và bổ nghĩa của bảng (`.unique()`, `.index()`, `.searchable()`, `.created()`,
`.updated()`) ở [`schema.md`](schema.md) mục 2, 3, 5. Mỗi bảng đúng một khoá. Không có `number()`: chọn rõ `integer()`,
`decimal(scale)` hay `float()`.

---

## 3. Truy vấn bằng Lambda (`kitwork/database`)

Cú pháp truy vấn thống nhất: **Mô tả đứng trước, hành động đứng cuối.** Biểu thức trong lambda được trình biên dịch dịch thành câu lệnh SQL có tham số hoá (`?`), không bao giờ nối chuỗi SQL từ đầu vào người dùng.

```js
import { db } from "app/core/shop.work";
import { sum, count } from "kitwork/database";

// 1. Đọc danh sách / một dòng / kiểm tra tồn tại / đếm
const activeOrders = db
  .where((orders) => orders.status === "moi" && orders.amount >= 100000)
  .descending((orders) => orders.createdAt)
  .list(20);

const order = db.where((orders) => orders.id === id).first();
const exists = db.where((users) => users.email === email).exists();
const total = db.where((orders) => orders.status === "giao").count();

// 2. Nối bảng (JOIN tự suy từ khoá ngoại users.id) và chọn hình dữ liệu
const rows = db
  .where((orders, users) => orders.status === "moi" && users.role === "member")
  .select((orders, users) => ({
    id: orders.id,
    customer: users.name,
    amount: orders.amount,
  }))
  .list();

// 3. Gom nhóm và hàm gộp (sum, count, avg, min, max)
const byStatus = db
  .group((orders) => orders.status)
  .select((orders) => ({
    status: orders.status,
    count: count(),
    revenue: sum(orders.amount),
  }))
  .list();

// 4. Tìm kiếm toàn văn trên các cột .searchable(): xếp theo độ khớp, dòng có thêm score và snippet
const results = db
  .where((posts) => posts.matches(queryText) && posts.published === true)
  .list(10);

// 5. Ghi dữ liệu: insert, upsert, update, delete
const created = db.insert((orders) => ({ userId: user.id, amount: 250000 }));

const updatedCount = db
  .where((orders) => orders.id === id && orders.status === "moi")
  .update((orders) => ({ status: "giao" }));

const deletedCount = db
  .where((orders) => orders.id === id)
  .delete();
```

---

## 4. Giao dịch (`transaction`), Lịch sử (`history`), `before` và `after`

- **Giao dịch (`db.transaction`):**
  ```js
  db.transaction((database) => {
    database.where((orders) => orders.id === id).update(() => ({ status: "giao" }));
    database.insert((payments) => ({ orderId: id, paid: amount, status: "ok" }));
  });
  ```
  Nếu lỗi từ `return error(...)` hoặc database thoát khỏi callback giao dịch, toàn bộ thay đổi trong khối được hoàn tác (rollback). Đặt `safe(() => db.transaction(...))` bên ngoài giao dịch để nhận lỗi sau khi rollback; nếu thu lỗi bằng `safe` ngay bên trong callback rồi trả thành công, giao dịch không tự rollback chỉ vì trước đó từng có lỗi. Hợp đồng lỗi theo [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe).
  Mọi câu ghi trong giao dịch đi qua handle `database` của hàm: ghi qua `db` trong lúc giao dịch là lỗi; đọc qua `db` được, trong snapshot riêng. Một giao dịch ghi một schema; giao dịch lồng chưa có.
- **Lịch sử và Khôi phục (`db.history(...)` và `db.at(...)`):**
  - Lịch sử là của từng bảng, do database khai: `WITH (history = on)` sau bảng trong file `.sql`, hay `ALTER TABLE … SET (history = on)` qua cổng database. Bảng tạo từ 07/11/2026 mặc định tắt; mọi bảng của file có từ trước thì bật. Không có `.history()` trong struct.
  - Bảng bật lịch sử: mọi lần ghi (`insert`, `update`, `delete`, kể cả qua cổng) ghi vào `kitwork_history` trong cùng giao dịch; host đặt số ngày giữ bằng `app.history({ days })` của `application.work` (30 ngày khi không khai).
  - Đọc dữ liệu hoặc lịch sử tại một thời điểm: `db.at(timestamp)` và `db.history((orders) => orders.id === id)`; trên bảng chưa bật lịch sử là lỗi, nói cách bật.
- **`before` và `after`:**
  - `.before(sự_kiện, (bảng) => …)` nhận hàng sắp ghi, trả hàng để ghi, hay `null` để không ghi (câu ghi 0 dòng, không lỗi). Sự kiện là `insert`, `update`, `delete`; `upsert` là `insert` hay `update` theo cái nó gặp.
  - `.after(sự_kiện, (bảng) => …)` nhận hàng đã ghi (hàng đã xoá với `delete`), chạy trong cùng giao dịch: lỗi thoát ra thì hoàn tác lần ghi; câu ghi qua handle của database trong nó vào cùng giao dịch.
  - Mỗi bảng một `before` và một `after` cho mỗi sự kiện; bảng là tên tham số của hàm, phải là bảng của `tables({ … })`.
  - `.policy(…)` (chính sách dòng) chưa có trong engine2: viết là lỗi ở chỗ viết.


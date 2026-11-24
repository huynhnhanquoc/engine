# Router

Đặc tả định tuyến theo cây thư mục, file `router.work` và `config.work` trong `engine2` (`library/router`).

## 1. Cây thư mục là cây URL

Đường dẫn thư mục bên trong một site ánh xạ trực tiếp sang đường dẫn URL. Thư mục của site là thư mục `module.routes` cho tên miền, hay gốc app khi `module.work` không viết `module.routes` ([`module.md`](module.md) mục 3):

```text
<site>/
├── config.work                 Luật chung của cả site (guard, limit, meta, reload...)
├── router.work                 Cửa của "/"
├── view.ui                     Khung trang của site
├── content.ui                  Nội dung trang "/"
├── san-pham/
│   ├── router.work             Cửa của "/san-pham"
│   ├── content.ui              Nội dung trang "/san-pham"
│   └── {id}/
│       ├── router.work         Cửa của "/san-pham/:id" (tham số chuỗi)
│       └── content.ui
└── don-hang/
    └── {orderId[number]}/
        └── router.work         Cửa của "/don-hang/:orderId" (chỉ khớp số nguyên)
```

### Luật đoạn đường dẫn và tham số động

- **Đoạn tĩnh** ưu tiên trước đoạn động cùng cấp.
- **Tham số động:** viết `{id}`, `{id[string]}` (khớp một đoạn bất kỳ không rỗng), `{id[number]}` (chỉ khớp chữ số `0-9`, giá trị là số) hoặc `{id(^regex$)}`. Kiểu khác ngoài `string` và `number` là lỗi lúc boot. Tên trong ngoặc vuông (`[id]`) là đoạn tĩnh, khớp đúng chữ `[id]`.
- **Đoạn bắt phần còn lại (Catch-all):** `{...path}` nhận phần còn lại của đường dẫn. Chưa kiểm lúc boot nó là thư mục lá; thư mục con của nó không bao giờ được tới.
- **Thư mục bắt đầu bằng `.` hay `_`** (`.shared/`, `.schedule/`, `.database/`, `.tunnel/`, `_nhap/`) không phải là route. Riêng `.well-known/` là route; file của nó được phục vụ trước route.
- **Chuẩn hoá đường dẫn:** `/san-pham/` và `/san-pham//` đều chuẩn hoá về `/san-pham` trước khi tra route, tra cache và đếm giới hạn tần suất.

---

## 2. Một dòng import, hai file cùng từ vựng

Cả `router.work` và `config.work` đều dùng chung đúng một dòng import ở đầu file:

```js
import * as router from "kitwork/router";
```

- **Chỉ `router.work` và `config.work` được import `"kitwork/router"`.** Import trong file khác là lỗi boot, lúc file ấy được nạp.
- **Không `export`** trong `router.work` và `config.work`.
- **`config.work` chạy trước `router.work`** trong cùng một thư mục. `config.work` chỉ chứa luật của nhánh (`guard`, `limit`, `language`, `meta`, `favicon`, `reload`), không khai báo cửa HTTP (`get`, `post`, `view`…).

---

## 3. Khai báo cửa (Doors) và Luật đứng trước cửa

Trong `router.work`, mỗi phương thức HTTP là một cửa, được khai báo tối đa **một lần mỗi thư mục** ở cấp cao nhất của file. Các luật (`limit`, `guard`, `cache`, `persist`) luôn **đứng trước cửa**:

```js
// san-pham/router.work
import * as router from "kitwork/router";
import { staff, authenticated } from "app/core/session.work";

router.cache("5m").get((context) => {
  return context.json({ items: [] });
});

router.limit({ rate: 20, per: "1m", by: "ip" })
  .guard(authenticated)
  .post((context) => {
    const body = context.request.json();
    return context.status(201).json({ created: body });
  });

router.guard(staff).delete((context) => {
  return context.status(204).text("");
});
```

### Các cửa của `router.work`

| Cửa | Phương thức | Ý nghĩa |
|---|---|---|
| `router.view(fn)` | `GET` (+ việc của trang) | Vẽ màn hình `content.ui` bằng `context.bind({ ... })` (xem [`view.md`](view.md)) |
| `router.get(fn)` | `GET` (và `HEAD`) | Trả dữ liệu (`json`, `text`, `file`), chuyển hướng (`redirect`), hoặc mở luồng `context.sse` |
| `router.post(fn)` | `POST` | Xử lý dữ liệu gửi lên hoặc form HTML |
| `router.put(fn)` | `PUT` | Thay thế tài nguyên |
| `router.patch(fn)` | `PATCH` | Cập nhật một phần tài nguyên |
| `router.delete(fn)` | `DELETE` | Xoá tài nguyên |
| `router.proxy(urlOrFn)` | `GET` | Chuyển tiếp câu trả lời từ một URL nguồn ở xa. Chưa phục vụ: bỏ qua kèm một dòng boot |

- Khai báo cả `router.view` lẫn `router.get` trong cùng một thư mục là lỗi lúc boot vì cả hai cùng chiếm cửa `GET`.
- Nối luật ở phía sau cửa (`router.get(fn).guard(x)`) là lỗi lúc boot: *"luật đứng trước cửa: viết `router.guard(x).get(fn)`"*.
- `router.cache(duration)` và `router.persist(duration)` đứng trước cửa; chưa phục vụ: bỏ qua kèm một dòng boot. Viết trước `.methods(...)` là lỗi boot.

### Cửa sinh sẵn ở gốc site

Chưa phục vụ: bỏ qua kèm một dòng boot.
- `router.sitemap(source)` → `/sitemap.xml`
- `router.rss(source)` → `/rss.xml`
- `router.robots(source)` → `/robots.txt`
- `router.llms(source)` / `router.llmsFull(source)` → `/llms.txt`, `/llms-full.txt`
- `router.manifest(source)` → `/manifest.json`

---

## 4. Luật của nhánh (`config.work` và `.methods(...)`)

Để áp dụng `limit` hoặc `guard` cho cả một nhánh thư mục (bao gồm thư mục hiện tại và mọi thư mục con), kết thúc chuỗi bằng `.methods(...)`:

```js
// quan-ly/config.work
import * as router from "kitwork/router";
import { staff } from "app/core/session.work";

router.limit({ rate: 300, per: "1m", by: "ip" })
  .guard(staff)
  .methods("*");

router.meta({ robots: "noindex" });
```

```js
// bai-viet/config.work: ai cũng đọc được, chỉ nhân viên được sửa/xoá
import * as router from "kitwork/router";
import { staff } from "app/core/session.work";

router.guard(staff).methods("post", "put", "patch", "delete");
```

- **`.methods(...)`** nhận các tên phương thức viết thường (`"get"`, `"post"`, `"put"`, `"patch"`, `"delete"`) hoặc `"*"` cho tất cả phương thức.
- Viết `router.guard(fn)` hoặc `router.limit(...)` đứng lơ lửng không có cửa hay `.methods(...)` ở cuối là lỗi lúc boot.
- **Thứ tự chạy cố định:** Chuỗi của gốc app → gốc site → thư mục cha → thư mục hiện tại (`config.work` trước, `router.work` sau) → chuỗi đứng ngay trước cửa → hàm của cửa.
- **Thư mục con kế thừa toàn bộ luật của cha và không bao giờ gỡ được.** Trang công khai (như `/login`) phải đặt ngoài thư mục bị khoá (`/quan-ly`).

### Các lời gọi cấu hình theo thư mục

| Lời gọi | Phạm vi cho phép | Ý nghĩa |
|---|---|---|
| `router.language("vi")` | Gốc app, gốc site, nhánh | Thuộc tính ngôn ngữ `lang` của trang |
| `router.meta({ title, description, image, robots })` | Gốc site, nhánh | Metadata mặc định kế thừa xuống các trang con, mỗi khoá là chữ |
| `router.favicon("/assets/favicon.svg")` | Gốc app, gốc site, nhánh | Icon các trang của thư mục link tới; ở gốc site (hay gốc app) `/favicon.ico` trả file ấy. File dấu chấm hay nguồn (`.work`, `.ui`) là lỗi boot; có cả file `favicon.ico` ở gốc site khác nguồn là lỗi boot |
| `router.reload(true \| false)` | Chỉ gốc site | Không viết hay `false`: điều hướng giữ khung (Drive), không tải lại toàn trang; `true`: tải lại cả trang. Viết ở nhánh là lỗi boot; link cần tải lại cả trang viết `reload="true"` |

Múi giờ của ứng dụng là `module.timezone(…)` trong `module.work` ([`module.md`](module.md)); `router.timezone(…)` là chữ cũ, lỗi nêu chữ mới. "Gốc app" ở bảng trên là gốc của app có `module.routes`; app không có `module.routes` là một site, gốc app là gốc site.

*(Lưu ý: 7 từ thiết kế giao diện gồm `colors`, `gradients`, `fonts`, `highlight`, `appearance`, `contrast`, `transition` nằm trong `design.work` của thư viện `"kitwork/design"` — xem [`design.md`](design.md)).*

---

## 5. Guard (`router.guard`)

Mọi nhánh thực thi của hàm guard phải trả về qua `context`:

```js
export const staff = (context) => {
  const user = currentUser(context);
  if (!user) {
    return context.redirect("/login");
  }
  if (user.role !== "staff") {
    return context.error("Chỉ nhân viên mới có quyền truy cập"); // Mặc định 403 trong guard
  }
  return context.next();
};
```

- **`return context.next()`:** Cho phép đi tiếp tới guard kế tiếp hoặc vào cửa. `context.next()` chỉ hợp lệ trong lệnh `return` của guard.
- **`return context.error(message?)`:** Chặn lại với mã `403 Forbidden` (hoặc mã `4xx` do `context.status(401).error(...)` chọn trước đó).
- **`return context.redirect(url)`:** Chuyển hướng (mặc định `302 Found`).
- **`return context.json(data)` / `context.text(str)`:** Trả lời trực tiếp và dừng chuỗi.
- **Hỏng thì đóng:** Guard viết sai là lỗi boot ở đúng dòng: một nhánh đi tới cuối guard không `return`; giá trị viết thẳng sau `return` (`true`, `false`, `null`, `undefined`, chuỗi, số, phép so sánh…, kể cả sau `?:`, `||`, `&&`); `return` không giá trị; `context.next()` ngoài `return` của guard; tham số `next`; vẽ trang (`context.view`, `context.bind`). `context.next()` trong hàm của cửa cũng là lỗi boot. Boot đọc guard viết tại chỗ, của một `const` cấp file, hay import từ file của app. Thứ boot không thấy (giá trị qua một tên hay một lời gọi) bị từ chối lúc chạy với `403`, lý do vào log; trang vẽ qua một lời gọi (`context.view`, `context.bind` trong hàm guard gọi) cũng vậy.

`context.error(...)` tạo câu trả lời để chặn yêu cầu. Hàm nghiệp vụ dùng chung báo thất bại bằng `return error(message)` và nơi gọi có thể thu lỗi bằng `safe(() => ...)`; hai builtin không cần import. Lỗi nghiệp vụ chưa xử lý trong guard cũng phải chặn yêu cầu, không được coi là cho phép đi tiếp. Phân biệt hai loại kết quả theo [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe).

---

## 6. `context`, `request` và `response`

Hàm của cửa và của guard nhận một tham số `context` (có thể destructure `{ request, response }`):

### Đọc yêu cầu (`context.request` và các phương thức tắt trên `context`)

| Cú pháp | Giá trị trả về |
|---|---|
| `context.request.method` | Chuỗi phương thức viết hoa (`"GET"`, `"POST"`…) |
| `context.request.url` | URL đầy đủ: scheme, host, đường dẫn, query |
| `context.request.path` | Đường dẫn của request |
| `context.params("id")` / `context.request.params("id")` | Giá trị tham số động từ đường dẫn thư mục (`{id}`; số với `{id[number]}`) |
| `context.query("page")` / `context.request.query("page")` | Tham số trên query string (`?page=2`), `""` khi không có |
| `context.request.header("Accept")` | Giá trị header HTTP (không phân biệt hoa thường) |
| `context.cookie("session")` | Cookie trình duyệt gửi, `""` khi không có. `context.request.cookie(...)` là lỗi chỉ sang cách viết này; `context.request.cookies` là mọi cookie |
| `context.request.json()` | Giải mã thân request từ JSON |
| `context.request.form()` | Đọc dữ liệu form (`application/x-www-form-urlencoded` hoặc `multipart/form-data`) |
| `context.request.text()` | Đọc thân request dạng chuỗi UTF-8 |

### Tạo câu trả lời (`context` / `context.response`)

Các bước chỉnh sửa đứng trước, hành động kết thúc đứng cuối:

```js
return context
  .status(201)
  .header("X-Request-Id", "req-1")
  .cookie("session", token, { maxAge: "7d", path: "/" })
  .json({ ok: true });
```

- **Chỉnh sửa (đứng trước):**
  - `.status(code)` (mã HTTP 200–599)
  - `.type(mediaType)` (`Content-Type`; trước `.json(...)` là lỗi)
  - `.header(name, value)` / `.headers({ ... })`
  - `.cookie(name, value, options?)` (mặc định bật `HttpOnly`, `SameSite=Lax`, `Path=/`, và `Secure` trên HTTPS; truyền `null` vào `value` để xoá cookie). Một đối số, `context.cookie(name)`, là đọc cookie
  - `.title(str)` / `.description(str)` / `.sync({ ... })` (đứng trước `.bind(...)` trong `router.view` — xem [`view.md`](view.md))
  - `.meta(...)`, `.image(...)`, `.canonical(...)`, `.robots(...)`: nhận nhưng chưa dùng (bỏ đối số)
- **Hành động kết thúc (trả về câu trả lời cuối cùng):**
  - `.bind({ ... })` (hoặc `.sync({ ... }).bind({ ... })`) — chỉ dùng trong `router.view` và các cửa `post`, `put`, `patch`, `delete` cùng thư mục với `router.view`, vẽ `content.ui`; cửa khác là `500`.
  - `.view(data?)` — vẽ `content.ui` của thư mục với các tên của object `data` (trang không trạng thái, không việc). Thư mục không có `content.ui` thì `context.view(...)` viết trong hàm của cửa là lỗi boot; gọi từ hàm khác là `500` lúc chạy.
  - `.json(value)` — trả về JSON (`application/json; charset=utf-8`).
  - `.text(str)` — trả về văn bản thuần (`text/plain; charset=utf-8`).
  - `.html(str)` — trả về HTML (`text/html; charset=utf-8`).
  - `.svg(str)` — trả về SVG (`image/svg+xml; charset=utf-8`).
  - `.file(handleOrPath)` — phục vụ tệp tin (xem [`file.md`](file.md)).
  - `.redirect(url)` — chuyển hướng (mặc định `302`, hoặc mã `3xx` đặt bằng `.status(...)` trước đó), chỉ tới đường dẫn của site hay URL cùng host; host khác là lỗi.
  - `.notFound()` — trả về `404` với chữ `Not Found`. `notfound.ui` chỉ vẽ cho đường dẫn không thư mục nào trả lời (và `detail()` không thấy dòng).
  - `.error(message?)` — trả về `500` trong cửa, `403` trong guard, hay mã `4xx`/`5xx` đặt bằng `.status(...)` trước đó (guard chỉ `4xx`), thân là chữ `message` (không có thì chữ của status). `error.ui` chưa vẽ.
  - Lỗi không bắt bằng `safe` mà thoát khỏi handler: `500`, trừ `INVALID_INPUT` (dữ liệu từ ngoài, như `struct.parse` của [`schema.md`](schema.md)) là `400`; lý do vào log, không vào câu trả lời (06/11/2026).
  - Thân rỗng: `.status(204).text("")`.
  - `.sse` — mở luồng sự kiện Server-Sent Events trong `router.get` (xem [`sse.md`](sse.md)).

### Kiểm tra nguồn gốc (Origin Check) tự động

Mọi request ghi (`POST`, `PUT`, `PATCH`, `DELETE`) tới `router.work` từ trình duyệt đều được kiểm tra header `Origin` / `Sec-Fetch-Site` khớp với `Host` của site trước khi chạy handler, chặn tấn công CSRF từ trang web khác.


---

## Trạng thái triển khai (16/11/2026)

engine2 làm `guard`, `limit`, `.methods(...)` của mục 3–5 (`library/router/rules.go`, `host/site.go`). Chỗ trên chưa nói, engine
theo `docs/KITWORK_ROUTER.md` (chốt 28/10): `.methods(...)` viết được cả trong `router.work`; chuỗi của nhánh chạy cả cho đường
dẫn không thư mục nào trả lời và method không có cửa, trước 404 và 405, nên guard của `/quan-ly` chặn cả `/quan-ly/khong-co`, và
guard trả lời là cửa của cả nhánh với các method nó ghi; `OPTIONS` không gặp luật nào. `by` là `"ip"` (mặc định, địa chỉ của kết
nối) hay `"global"`, `"user"` là lỗi boot chưa phục vụ; limit trước `.methods(...)` đếm chung cả nhánh, trước cửa đếm riêng từng
đường dẫn đã chuẩn hoá; vượt trần là `429` kèm `Retry-After`. `cache`, `persist` trước `.methods(...)` là lỗi boot. Engine chọn
thêm: `context.error(...)` trong guard chỉ nhận mã 4xx đặt trước (5xx là lỗi); guard và hàm của cửa chung năng lượng và thời gian
của một request. Guard viết sai (mục 5) là lỗi boot từ 16/11 (`core/compiler/router_guard.go`, `host/boot.go`); thứ boot không
thấy là `403` lúc chạy, lý do vào log; `context.next()` gọi ngoài guard mà boot không thấy là lỗi lúc chạy. Chưa làm: lỗi boot khi
guard của cha luôn trả lời và che cửa của thư mục con. `cache`, `persist`, cửa sinh sẵn và `proxy` chưa phục vụ: bỏ qua kèm một
dòng boot.

`context.cookie(name, value, options?)` làm từ 08/11 (`library/router/bind.go`): mặc định `HttpOnly`, `SameSite=Lax` và `Secure`
trên HTTPS và `Path=/` như mục 6 nói. `value` là chuỗi hay số gồm các ký tự cookie mang được nguyên (không
khoảng trắng, phẩy, chấm phẩy, ngoặc kép, gạch chéo ngược), `null` xoá cookie. `options` nhận `maxAge` (khoảng, `"7d"`), `path`,
`domain`, `secure`, `httpOnly`, `sameSite` (`"lax"`, `"strict"`, `"none"`, cái sau cần `secure: true`); khoá lạ là lỗi. Một cookie
đặt hai lần thì lần sau thắng. `context.cookie(name)` một đối số đọc cookie trình duyệt gửi; `context.request.cookie(name)` là lỗi
chỉ sang nó (16/11, `library/router/context.go`, `request.go`).

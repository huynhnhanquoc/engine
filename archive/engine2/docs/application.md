# Application

Đặc tả file khởi chạy `application.work` và vòng đời của host trong `engine2` (`library/application` và `host/`).

## 1. Cấu trúc tổng quan

Mỗi tiến trình Kitwork khởi động từ một file `application.work` ở thư mục gốc của dự án, được nạp qua `engine2.Run(file)` (hoặc kiểm tra tĩnh qua `engine2.Check(file)`).

```js
// application.work
import * as app from "kitwork/application";
import * as file from "kitwork/file";

const env = file.env("./.env");

app.root("apps");
app.website({
  hostname: "kitwork.org",
  port: env.PORT || 8080,
  allowLocal: env.ALLOW_LOCAL || false,
});
app.run();
```

### Luật của `application.work`

- **Chỉ `application.work` được import `"kitwork/application"`.** Một file `.work` khác import nó thì bị từ chối lúc boot (app đó đóng).
- **Một dòng import chuẩn:** `import * as app from "kitwork/application";`.
- **Chỉ khai báo và khởi chạy:** `application.work` đọc biến môi trường của host (`file.env("./.env")`), khai báo thư mục gốc (`app.root`), các bề mặt (`app.website`, `app.database`, …), danh mục database (`app.databases`), giới hạn (`app.limits`), lệnh (`app.commands`), và kết thúc bằng `app.run()`.
- **`app.run()` là dòng cuối:** viết `app.x()` sau nó là lỗi boot. Không có `app.run()` thì manifest chỉ được đọc và kiểm, không mở gì.
- **Manifest phải khai ít nhất một bề mặt** (`app.website`, `app.database`, `app.desktop`, `app.mobile`), không thì là lỗi boot.
- **Không `export`:** Không file nào import `application.work`.

---

## 2. Thư mục ứng dụng (`apps/`)

engine2 phục vụ hai hình:

| Hình | Cấu trúc | Ghi chú |
|---|---|---|
| **Nhiều ứng dụng (Multi-tenant)** | `apps/<identity>/module.work`, `apps/<identity>/...` | Mỗi `<identity>` là một app, gốc của nó là thư mục chứa `module.work` ([`module.md`](module.md)), `.database/` riêng; site của nó do `module.work` và bảng `domain` của host nói |
| **Một ứng dụng** | `module.work` cạnh `application.work`, hay `module.work` không có `application.work` | Thư mục của manifest là app, app duy nhất; không bảng nào của host được đọc (từ 17/11/2026, [`module.md`](module.md) mục 7). `app.root` có tên thư mục `app` (hình `app/<domain>/` của 22/10/2026) là lỗi boot |

- `app.root(path)` chọn thư mục chứa các ứng dụng; không viết thì là `apps/` cạnh manifest.
- Một app lỗi chỉ đóng app đó (các site của nó trả 503); host vẫn chạy các app khác.
- Mỗi `apps/<identity>/` phải có `module.work`; thiếu thì app ấy đóng, lỗi nói phải viết gì ([`module.md`](module.md)).
- Import `"app/..."` trong bất kỳ file `.work` nào trỏ về thư mục gốc của chính ứng dụng (thư mục chứa `module.work`) chứa file đó.

---

## 3. Từ vựng của `kitwork/application`

Mỗi chữ được phục vụ viết một lần (hai lần là lỗi), trước `app.run()`. Chữ không có trên `app` (ví dụ `app.websit(...)`, `app.web(...)`) là lỗi đọc manifest ở dòng ấy, kèm chữ gần nhất (`did you mean app.website?`). Chữ có trong từ vựng mà engine2 chưa phục vụ in một dòng log lúc boot (`app.desktop() is not served by engine2 yet — ignored`), chuỗi vẫn chạy.

### 3.1. `app.root(path)`

Đặt thư mục gốc chứa các ứng dụng. Đường dẫn tương đối tính từ thư mục của manifest; đường dẫn tuyệt đối cũng nhận. Boot kiểm thư mục ấy có thật khi manifest khai `app.website`.

### 3.2. `app.website({ hostname, port, … })`

Bề mặt web: host phục vụ các site của các app qua HTTP/HTTPS theo `Host` header.

```js
app.website({ hostname: "kitwork.io", port: 8080 });
app.website({
  hostname: env.SITE_HOSTNAME || "kitwork.io",
  port: env.PORT || 8080,
  allowLocal: env.ALLOW_LOCAL || false,
  rateLimit: { rate: 2000, ip: 120, browser: 240, period: "1s" },
});
app.website("kitwork.io"); // chỉ tên: manifest không mở gì
```

- **`hostname`:** site mà `localhost`, `127.0.0.1`, `::1` tới khi `allowLocal` bật; không kèm scheme, port hay `/`.
- **`port`:** số nguyên `1`–`65535`; `app.run()` cần nó. Với AutoSSL, host trả HTTPS ở `443`, cổng `80` cho thử thách của Let's Encrypt, còn `port` là HTTP thường chỉ ở `127.0.0.1`; với `allowLocal` hay `tls: false`, `port` mở trên mọi địa chỉ.
- **`allowLocal`:** `true`/`false`; bật thì `<domain>.localhost` tới site `<domain>`, không AutoSSL.
- **`tls`:** mặc định AutoSSL, không cần viết; `tls: false` không phục vụ HTTPS; `tls: { email }` là địa chỉ Let's Encrypt viết tới.
- **`rateLimit`:** `{ rate, ip, browser, period }`, trần request của host, đếm trước khi tìm site; `user` là lỗi (chưa phục vụ).
- **`domains`:** file SQLite chứa bảng `domain` của host; mặc định `.database/system/kitwork.db` tính từ thư mục manifest.
- **`canonical`:** đã bỏ, viết là lỗi.
- `redirects`, `trustProxy`, `maxBody`: chưa phục vụ, một dòng log. Khoá khác là lỗi.

Tên miền của một app là những tên bảng `domain` giao cho `<identity>` (trạng thái đã xác nhận, chưa xoá, không chuyển đi), và, khi `module.work` viết `module.routes`, những thư mục ở gốc app mang tên của máy, mạng riêng hay test (`localhost`, `*.localhost`, `*.test`), không cần dòng; thư mục của mỗi tên do `module.routes` nói, không có thì gốc app là site của mọi tên ([`module.md`](module.md) mục 3). Thiếu bảng mà có thư mục mang tên công khai dưới `apps/` là lỗi boot. `www.X` và `X` tự chuyển về site có thật, không cần chữ nào. Tên không site nào có trả 404.

### 3.3. `app.run()`

Công tắc ở dòng cuối, không đối số. Lệnh đang chạy mở những gì các dòng trên khai mà nó mở được: `kitwork run` / `go run .` mở website trên `app.website({ port })` cùng cổng database; `kitwork database` chỉ mở cổng database. `app.run(8080)` là lỗi: port là của `app.website`.

```js
app.run();
```

### 3.4. Các lời gọi khác

| Lời gọi | Ý nghĩa |
|---|---|
| `app.hotReload(true \| false)` | `true`: khi host đang phục vụ, app có file đổi thì boot lại (đọc file mỗi 0,5 s); `false` hay không viết: đọc app một lần |
| `app.prewarm("kitwork.io", …)` · `app.prewarm("*")` · `app.prewarm(false)` | Site đọc sẵn lúc boot: trang ghép trước khi phục vụ, file tĩnh tới 256 KB (tối đa 32 MB mỗi site) giữ trong bộ nhớ, so với đĩa nhiều nhất mỗi giây một lần. Mặc định không site nào |
| `app.limits({ memory, energy, requests, host })` | Trần cho mọi site: `memory` một lượt chạy tạo được (`"1mb"`–`"16gb"`, mặc định 64 MiB), `energy` một lượt chạy tiêu được (`100_000`–`1_000_000_000_000`, mặc định `10_000_000`), `requests` số request một site chạy cùng lúc (mặc định gấp đôi số nhân, ít nhất 8; quá thì chờ 1 s rồi 503), `host` tổng byte mọi lượt chạy của host (mặc định không giới hạn; hết thì 503) |
| `app.limits("shop.test", { … })` | Đã bỏ (17/11/2026): lỗi chỉ sang bảng `tenant`. Trần riêng của một tenant là dòng của nó trong bảng `tenant` của database `system` ([`module.md`](module.md) mục 6) |
| `app.logger({ level, format, logfile, errorfile, max_size, max_backups, max_age, compress, console })` | Nơi ghi log ngoài stdout: `level` (`"info"`), `format` `"text"`/`"json"`, file xoay vòng theo MB, số bản, số ngày (mặc định 10, 5, 28) |
| `app.commands({ tên: (context) => … })` | Lệnh của host: `kitwork <tên>` chạy hàm trong tiến trình đọc manifest, không boot site, `context.arguments`, `context.options`, `context.print(…)`. Tên viết thường, số, gạch ngang, bắt đầu bằng chữ, không trùng chữ của kitwork (`run`, `dev`, `check`, `migrate`, `database`, `version`, `help`, `new`, `create`). Chữ không lệnh nào nhận là lỗi, không bao giờ mở server |
| `app.database(path, { host, port, user, password, concurrency, search, tls, channelBinding, clientAuthority })` | Cổng PostgreSQL wire trên các file SQLite của thư mục `.database/` của host (`path`) và của mọi app; đọc và ghi theo `GRANT`, qua người ghi KitDB. `port`, `password` bắt buộc; `user` mặc định `kitdb`; `host` mặc định `127.0.0.1`, ngoài máy chỉ qua TLS. Xem [`kitdb.md`](kitdb.md) mục 5 |
| `app.databases({ tên: { sqlite: path } \| { postgresql: url } \| { mysql: url }, apps })` | Danh mục database của host cho `connection("tên")`. `apps: ["shop.example"]` (tên một site của app hay thư mục app) hoặc `apps: "*"`; không viết `apps` thì không app nào mở được. `application`, `storage`, `files` là tên giữ. Hôm nay `connection("tên")` chỉ chạy trên tên `sqlite` |
| `app.timezone("Asia/Ho_Chi_Minh")` | Múi giờ của mọi app mà `module.work` không khai (17/11/2026): lịch không viết `.timezone(…)`, `Date`, mốc giờ của cache theo nó; app có `module.timezone(…)` thì theo của app. Tên IANA, một lần, `"Local"` là lỗi. Không viết thì UTC ([`module.md`](module.md)) |
| `app.history({ days })` | Số ngày giữ lịch sử của các database (`1`–`36500`, mặc định `30`); `at()` lùi tới đó |
| `app.desktop(…)`, `app.mobile(…)` | Bề mặt máy tính, di động: chưa phục vụ: một dòng log lúc boot, chuỗi vẫn chạy |
| `app.proxy(…)`, `app.policy(…)`, `app.shareEnvironment(…)`, `app.gateway(…)` | Chưa phục vụ: một dòng log lúc boot, chuỗi vẫn chạy |
| `app.postgresql(…)`, `app.mysql(…)`, `app.sqlite(…)`, `app.redis(…)`, `app.connect(…)` | Chưa phục vụ: một dòng log lúc boot, chuỗi vẫn chạy |

---

## 4. Vòng đời khởi chạy (`Read` → `Boot` → `Serve`)

1. **`host.Read(file)`:**
   - Biên dịch và chạy `application.work` với trần mặc định của một lượt chạy (`MaxEnergy = 10.000.000`, 64 MiB).
   - Gom toàn bộ các biến môi trường thiếu từ `env.require(...)` trong `application.work` thành một thông báo lỗi duy nhất nếu có.
2. **`Host.Boot()`:**
   - Kiểm manifest: có ít nhất một bề mặt, website mà `app.run()` mở có `port`, `app.root` là thư mục.
   - Đọc bảng `domain`, rồi duyệt từng app `apps/<identity>/`: `module.work` trước tiên, rồi lượt khai bảng (mọi file `.work` thường), `design.work` và `config.work` của gốc app (khi có `module.routes`), lượt nạp job và lịch (mọi file `.work` thường), rồi mỗi site từ thư mục gốc xuống các thư mục con, `config.work` trước, `router.work` sau ở mỗi thư mục.
   - Kiểm tra mọi ràng buộc tĩnh (tồn tại `content.ui` khi có `router.view`, không trùng cửa HTTP, tất cả biến `env.require` của app đều có mặt). Một app lỗi chỉ đóng app đó; site của nó trả 503. Một tên mà `module.routes` không cho thư mục hợp lệ chỉ đóng site của tên ấy.
   - Đóng băng (`Freeze`) giá trị cấp file của mọi file đã nạp.
   - `engine2.Check(file)` dừng sau `Boot()` mà không mở cổng mạng, và báo lỗi của các app bị đóng — dùng trong CI/CD và công cụ kiểm tra trước khi triển khai.
3. **`Host.Serve()`:**
   - Cần `app.run()`. Mở cổng database (`app.database`), chạy hàng đợi và lịch của các app, rồi mở cổng HTTP (và HTTPS `443` + `80` khi AutoSSL) với các giới hạn thời gian:
     - `ReadHeaderTimeout`: `10s`
     - `ReadTimeout`: `30s`
     - `IdleTimeout`: `2m`
     - Thời hạn chạy mỗi request (`RequestTimeout`): `30s`
   - Ctrl+C hay `SIGTERM` (`engine2.Run` bắt): đóng cổng nhận kết nối mới, đóng luồng `context.sse`, chờ các request đang chạy hoàn tất trong tối đa `10s`, rồi dừng hàng đợi, lịch, routine và đóng các file database. Tín hiệu thứ hai thoát ngay.

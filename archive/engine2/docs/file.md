# File

Đặc tả đọc biến môi trường (`file.env`) và quản lý tệp tin của ứng dụng qua thư viện `"kitwork/file"` (`library/file`).

## 1. Biến môi trường (`file.env`)

Kitwork không có đối tượng `process.env` toàn cục. Mọi biến môi trường được đọc tường minh qua `file.env(...paths)` từ `"kitwork/file"`:

```js
import * as file from "kitwork/file";

// Các file .env đọc theo thứ tự: file viết sau thắng file viết trước, từng khoá
const env = file.env("app/.env", "./.env");

const port = env.PORT || 8080;                     // Tự động ép kiểu: 3000 → số 3000
const crmUrl = env.require("CRM_URL");             // Bắt buộc có mặt lúc boot
const smtpKey = env.require("SMTP_KEY");
```

### Luật của `file.env` và `Environment`

1. **Cách ly bằng `os.OpenRoot`:**
   - Trong app, đường dẫn truyền vào `file.env(...)` được giải quyết bên trong thư mục gốc của ứng dụng: `"app/.env"` trỏ tới `.env` ở gốc ứng dụng (`apps/<identity>/`), còn `"./.env"` trỏ tới `.env` ở cùng thư mục với file `.work` đang viết lời gọi. Manifest (`application.work`) chỉ viết `"./…"`, tính từ thư mục của nó.
   - Đường dẫn chứa `..` hoặc trỏ ra ngoài gốc (kể cả qua liên kết) bị từ chối ngay lập tức. File không có thì không có khoá nào.
   - `file.env` chỉ gọi lúc boot, ở đầu file; trong một request là lỗi.
2. **Thứ tự ưu tiên:**
   - Các file liệt kê trong `file.env(path1, path2, ...)` đọc từ trái sang phải: file sau ghi đè khoá của file trước, từng khoá; `KEY=` rỗng ở file sau giữ giá trị của file trước.
   - Chỉ trong manifest, biến môi trường của hệ điều hành thắng mọi file (một dòng log khi `.env` đặt khoá hệ điều hành cũng có). App không bao giờ thấy biến của hệ điều hành.
   - **Lưu ý (manifest):** Không đặt tên khoá trùng với biến chuẩn của hệ điều hành (`PATH`, `HOME`, `USER`, `HOST`, `HOSTNAME`, `TEMP`, `TMP`, `SHELL`, `OS`) vì hệ điều hành luôn có sẵn chúng.
3. **Ép kiểu tự động nhất quán (Canonical Coercion):**
   - `true`, `false` (mọi kiểu hoa thường) → boolean.
   - Số ở dạng chuẩn, viết lại ra đúng chữ ấy (`8080`, `-7`, `3.14`) → `number`; `0123`, `+84…`, `1e3`, `1.10`, `-0`, `Infinity`, `NaN` giữ là chuỗi.
   - Giá trị trong dấu nháy là chuỗi.
   - Chuỗi rỗng hoặc khoá không tồn tại → `null` (nhờ đó `env.PORT || 8080` hoạt động đúng ngay cả khi viết `PORT=` trống trong `.env`).
4. **Kiểm tra gom nhóm với `env.require(key)`:**
   - Nếu một hoặc nhiều biến gọi `env.require("KEY")` bị thiếu hoặc rỗng, engine không dừng ở biến đầu tiên mà **gom tất cả các biến thiếu của app lại thành một thông báo lỗi duy nhất lúc boot** (`env.require: no file sets KEY (file:dòng:cột), …`), liệt kê từng khoá kèm chỗ gọi, không in giá trị. Sau boot, khoá thiếu là lỗi `NOT_FOUND` của lời gọi.

---

## 2. Quản lý tệp tin của ứng dụng (`kitwork/file`)

Luật của từ vựng:

- đọc thì gọi thẳng, `file.read`, `text`, `bytes`, `json`, `list`, `info`, `exists`; đổi thì `open()` trước,
  `file.open(path).write(data)`, `create()`, `copy(đích)`, `move(đích)`, `delete()`, `clear()`. Không có `file.write(path, …)`,
  `file.delete(path)`;
- kết quả là giá trị (`write` trả `FileInfo`), lỗi là lỗi của Work mà `safe` bắt, không có `{ ok, error }`; đọc một file không
  có trả `null`;
- `handle.exists()` là hàm; mọi thành viên của handle là hàm;
- upload là `context.request.file("avatar")`, `request.files(…)`, không phải `form().avatar`;
- vùng code chỉ viết `app/…`; `"./"` là cách viết của `file.env`, không phải của các hàm đọc file;
- tên lấy từ bên ngoài ghép bằng `file.open("invoices/").segment(id)` (mỗi đối số một tên, không ra khỏi thư mục), không
  cộng chuỗi `"invoices/" + id`.

Ngoài `file.env`, thư viện `"kitwork/file"` cung cấp API làm việc với tệp tin tĩnh của ứng dụng (`app/...`) và kho tệp người dùng tải lên (`.database/files/...`):

```js
import * as file from "kitwork/file";

// 1. Lưu một tệp từ form tải lên vào kho .database/files/
const avatar = context.request.file("avatar");      // null khi form không có
if (avatar === null) {
  return context.status(400).json({ error: "thiếu ảnh" });
}
const saved = file.open("avatars/").segment(user.id + ".png").write(avatar);   // FileInfo; lỗi là lỗi của Work

// 2. Phục vụ tệp qua route có kiểm tra quyền (io.Copy ở tầng Go)
router.guard(authenticated).get((context) => {
  const handle = file.open("invoices/").segment(context.params("id") + ".pdf");
  if (!handle.exists()) {
    return context.notFound();
  }
  return context.file(handle);
});

// 3. Đọc nội dung tệp văn bản / JSON hoặc xoá tệp
const text = file.text("templates/welcome.txt");   // null khi không có
const settings = file.json("settings.json");
const removed = file.open("avatars/").segment(user.id + ".png").delete();   // false khi không có gì để xoá
```

### Ranh giới an toàn tệp tin

- **Hai không gian tách biệt:**
  - Đường dẫn bắt đầu bằng `"app/"` đọc tệp tĩnh trong cây mã nguồn của ứng dụng (chỉ đọc); `"./"` không phải đường dẫn của các hàm đọc file.
  - Đường dẫn thông thường (`"avatars/1.png"`) nằm trong `.database/files/` của ứng dụng đó (đọc, ghi, xoá).
- **Không bao giờ phục vụ tự động `.database/files/`:** Tệp trong `.database/files/` chỉ ra ngoài khi một route trả `context.file(đường_dẫn)` hay `context.file(handle)`; kiểm quyền (`router.guard`) là việc của route.
- **Khoá đường dẫn ở mức OS (`os.OpenRoot`):** Không một đường dẫn hay symlink nào có thể thoát ra ngoài thư mục của tenant hoặc chạm vào `.database/application/` (database), `.database/tunnel/` (khoá bí mật) hay file `.env`.

---

## Trạng thái triển khai (09/11/2026)

engine2: `library/file` (`module.go`, `handle.go`, `upload.go`),
`request.file`, `context.file` của `library/router`, `internal/safepath`. Phần chạm đĩa (`path.go`, `zone.go`, `copy.go`,
`kind.go`) dời nguyên từ `utilities/file` của engine cũ cùng test của nó.

- **Hai vùng:** vùng dữ liệu `.database/files/` của app, sinh lúc ghi lần đầu, không bao giờ được phục vụ tự động; vùng code
  `app/…`, chỉ đọc, `.env`, `.env.*`, `.database/` không bao giờ tới được (theo tên như viết và theo tên trên đĩa sau khi giải
  liên kết). Mọi thao tác qua `os.Root` của thư mục app rồi của vùng: một liên kết dẫn ra ngoài là `DENIED`.
- **Đọc:** `read` giải mã theo đuôi (`.json` giá trị JSON, đuôi chữ chuỗi UTF-8, còn lại bytes), `text`, `bytes`, `json`; không có
  file trả `null`; JSON hỏng, byte không phải UTF-8 là `INVALID_INPUT`; trần đọc vào VM 8 MiB (`TOO_LARGE`). `list()` con trực
  tiếp theo tên, `info()` `FileInfo` hay `null`, `exists()`.
- **Đổi:** chỉ trên handle, và không lúc file boot (đọc thì được). `write` theo đuôi (JSON viết được, chuỗi UTF-8, bytes hay
  upload; chuỗi `"data:…"` không giải mã ngầm), ghi ra file tạm rồi đổi tên; `copy`, `move` không ghi đè, tạo thư mục cha,
  `copy` từ `app/` sang vùng dữ liệu được; `delete` không đệ quy; `clear` dừng sau 10 000 mục hay khi request xong, `PARTIAL`
  với `error.progress` (lỗi mang số đã làm, `safe` đưa ra như `fields`).
- **Upload:** `request.file(tên)`, `request.files(tên)` đọc thân multipart một lần, theo luồng, trần 32 MiB của engine cũ
  (413 khi vượt), giữ 64 KiB trong bộ nhớ, phần còn lại ra file tạm của hệ thống, xoá khi request xong; `write(upload)` chép
  bằng `io.Copy`. `request.form` đọc sau đó lấy các trường của lần đọc ấy, chỉ tính byte của chúng vào trần 1 MiB; `text()`,
  `json()`, `bytes()` sau đó là `INVALID_INPUT` vì thân đã thành upload. Upload thuộc request: routine không giữ được.
- **Trả file:** `context.file(path)`, `context.file(handle)` phục vụ thẳng từ đĩa (`Range`, `ETag`, `Last-Modified`, 304,
  `nosniff`), media type theo đuôi trừ khi `.type(…)` đứng trước, không đuôi là `application/octet-stream`; ngoài vùng, `.env`,
  thư mục, không có: 404 như nhau. Đường dẫn sai dạng là `INVALID_ARGUMENT` và không chiếm chỗ câu trả lời.
  `context.file(bytes)` gửi nguyên bytes.
- **Chưa có:** hạn mức mỗi app, trần upload theo route, cảnh báo đường dẫn lấy từ request, nơi lưu dùng chung
  nhiều máy, desktop, mobile; `clear()` và chép thư mục không tốn energy; `createdAt` chỉ có trên Windows. Một upload `.html`
  hay `.svg` trả bằng `context.file` từ vùng dữ liệu đi ra kèm `Content-Security-Policy: sandbox`, không chạy script trong
  origin của site (engine2 1441679).

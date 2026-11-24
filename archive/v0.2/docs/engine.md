# Engine

Kiến trúc và nguyên tắc của `engine2` — engine thực thi ngôn ngữ `.work` và `.ui` của Kitwork.

## 1. Năm nguyên tắc nền tảng

1. **Một việc, một cách viết.** Không có hai từ khoá, hai cú pháp hay hai thư viện cho cùng một ý định. Từ cũ hoặc cách viết lệch chuẩn bị từ chối ngay lúc biên dịch hoặc lúc boot kèm gợi ý sửa chính xác.
2. **Hỏng thì đóng (Fail-Closed).** Thiếu cấu hình bắt buộc, sai kiểu dữ liệu, guard không trả lời rõ ràng, hay vượt giới hạn tài nguyên đều dừng ngay và từ chối yêu cầu. Không có chế độ tự đoán hay lặng lẽ bỏ qua.
3. **Kiểm tra tĩnh trước khi chạy.** Mọi thứ biết được từ mã nguồn (cú pháp, phạm vi biến `var`/`let`/`const`, tên hàm builtin, tên thuộc tính của thư viện, sự tồn tại của file `.ui`, kiểu cột schema) đều được kiểm tra ở bước `Compile` / `Boot` (`engine2.Check`), trước khi mở cổng nhận kết nối.
4. **Ranh giới cô lập theo ứng dụng (Tenant Isolation).** Mỗi ứng dụng (`app/` hoặc `apps/<identity>/`) có cây thư mục, biến môi trường, cơ sở dữ liệu (`.database/`), kho file và bộ nhớ đệm riêng. Đường dẫn file bị khoá ở mức hệ điều hành bằng `os.OpenRoot`.
5. **Chỉ thư viện chuẩn Go (Zero External Dependencies).** Engine hoàn toàn tự chủ: trình biên dịch, máy ảo bytecode và các thư viện `"kitwork/*"` chỉ dùng thư viện chuẩn của Go. **Ngoại lệ (`go.mod`):** KitDB (module `github.com/huynhnhanquoc/sqlitewire`, repo công khai riêng, `replace ../sqlitewire`; cổng `pgwire` ở trong nó) lưu trên SQLite qua `modernc.org/sqlite`, SQLite dịch sang Go thuần (không cgo); `golang.org/x/crypto` cho AutoSSL (`acme/autocert`, `host/certificates.go`).

---

## 2. Cấu trúc thư mục `engine2`

```text
engine2/
├── run.go                  Cửa vào (package engine2): Run, RunDatabase, Check, Migrate, Command, CommandNames
├── core/                   Lõi ngôn ngữ .work (không phụ thuộc ứng dụng web hay thư viện)
│   ├── value/              Hệ thống kiểu Value 32 byte (số giữ tại chỗ, không cấp phát), Expose[T]
│   ├── builtins/           Các đối tượng chuẩn của JS (Math, JSON, Object, Array, String, Number, Boolean, Date, console), error, safe
│   ├── compiler/           Lexer, Parser, kiểm tra tĩnh, gán slot biến cục bộ, phát sinh Bytecode
│   └── runtime/            Máy ảo stack-based (VM), quản lý frame, năng lượng, bộ đếm byte, huỷ theo context
├── host/                   Vòng đời ứng dụng: đọc application.work, nạp các site/folder, phục vụ HTTP, deadline
├── internal/               id, loopback, safepath: phần dùng chung của các thư viện, không phải "kitwork/*"
├── library/                Các thư viện "kitwork/*" gắn vào ngôn ngữ qua cơ chế Expose[T]
│   ├── application/        kitwork/application (manifest khởi chạy)
│   ├── file/               kitwork/file (biến môi trường .env và kho tệp tin)
│   ├── router/             kitwork/router (cửa HTTP, luật nhánh, request/response/context)
│   ├── napas/              kitwork/napas (mã thanh toán EMVCo VietQR)
│   ├── schema/             kitwork/schema (struct, kiểu và luật của trường: schema.md)
│   └── ...                 (các thư viện khác theo đặc tả)
├── test/                   Test từ ngoài gói (compiler, runtime, host, ui, …)
└── docs/                   Tài liệu đặc tả kỹ thuật của engine2
```

### Luật chiều phụ thuộc (Import Graph)

```text
core/value  ←  core/builtins  ←  core/runtime  ←  core/compiler
    ↑                                                    ↑
library/* ─────────────────────────────────────────  host  ←  run.go
```

- **`core/` không bao giờ import `library/*` hay `host/`.** Máy ảo và trình biên dịch hoàn toàn độc lập với web, HTTP hay cơ sở dữ liệu.
- **`library/*` dựa trên `core/`** (đa số chỉ `core/value`; `database`, `queue`, `storage`, `routine`, `ui` dùng cả `core/runtime` hay `core/compiler`), KitDB và thư viện chuẩn Go; vài thư viện dùng thư viện khác (`router` dùng `file`, `storage`; `schedule` dùng `queue`; `ui` dùng `component`, `design`, `schema`). Mỗi thư viện biểu diễn các đối tượng Go thành `value.Value` thông qua `value.Expose[T]()`.
- **`host/`** ghép `core` và `library/*` lại với nhau, cấp phát đối tượng gốc `Kitwork` (`host/kitwork.go`) vào `BUILTIN 0` cho mỗi file chạy.

---

## 3. Cơ chế gắn thư viện (`BUILTIN 0` và `value.Expose[T]`)

### 3.1. `import` từ `"kitwork/*"`

- Không có import `"kitwork"` trần. Mọi tính năng của hệ thống đi qua `"kitwork/<tên>"` (ví dụ: `"kitwork/router"`, `"kitwork/file"`, `"kitwork/database"`).
- Trình biên dịch dịch `import * as router from "kitwork/router"` thành `const router = kitwork.router`: đọc thuộc tính `router` trên `BUILTIN 0` (đối tượng `Kitwork` do `host` cung cấp).
- Quyền import được kiểm tra theo vai trò của file khi file chạy lúc boot (thuộc tính của `Kitwork` trả lỗi, `host/kitwork.go`), là lỗi boot:
  - Chỉ `application.work` được import `"kitwork/application"`.
  - Chỉ `router.work` và `config.work` được import `"kitwork/router"`. File mà chúng import, file thường của app (boot đọc chúng để tìm bảng, job, lịch), file khai component không được import `"kitwork/router"`.
  - `"kitwork/schema"` chỉ được import trong file thường của app, không trong file có vai ([`schema.md`](schema.md)).
  - Chỉ `module.work` được import `"kitwork/module"`, và `module.work` chỉ import nó ([`module.md`](module.md)).

### 3.2. Phản xạ có kiểm soát với `value.Expose[T]`

Engine gắn trực tiếp struct Go vào máy ảo qua `value.Expose[T]()` trong [`core/value/method.go`](../core/value/method.go): đọc `*T` một lần bằng reflect lúc boot, không cần viết mã nối thủ công cho từng hàm:

- **Thuộc tính (Property):** Phương thức Go có chữ ký `func (*T) X() Value` (không nhận đối số) được gọi ngay khi mã `.work` đọc thuộc tính `obj.x` (ví dụ: `request.url`).
- **Hành động / Phương thức (Method):** Phương thức Go có chữ ký `func (*T) X(...Value) Value` trở thành hàm gọi được `obj.x(...)` (ví dụ: `napas.dynamic()`); chữ ký `func (*T) X(Budget, ...Value) Value` cũng vậy, và được VM đưa trần của lượt chạy đang gọi. Nếu đọc mà không gọi `()`, máy ảo trả về một hàm đã buộc sẵn struct. Phương thức có chữ ký khác là của Go.
- **Tên chữ thường / camelCase:** Tên phương thức viết hoa đầu của Go (`ToAccount`) tự động ánh xạ sang tên camelCase trong JS (`toAccount`).
- **`MissingMember(name string) Value`:** Nếu struct cài đặt phương thức này, nó được gọi khi người viết truy cập một tên không có trong danh sách phương thức — dùng cho khoá động (`env.PORT` trên `Environment`) hoặc báo lỗi kèm gợi ý từ gần nhất (Levenshtein distance trên `Router`, `Application`, `Module`).
- **`Freeze()`:** Khi một file `.work` nạp xong ở bước boot, mọi giá trị cấp file được đóng băng (`vm.Freeze`, `core/runtime/freeze.go`); struct cài đặt `Freeze()` được gọi để tự đánh dấu. Struct không giữ trạng thái đổi được thì `Freeze()` không làm gì: mỗi bước của `napas.bank(...)` luôn chép ra một bản mới, bản cũ giữ nguyên, nên nhiều request đọc cùng lúc.

---

## 4. Mô hình thực thi và giới hạn tài nguyên

1. **Nạp một lần lúc Boot, chạy nhiều lần lúc Request:**
   - Lúc `Boot`, các file `.work` được biên dịch ra `compiler.Bytecode` / `runtime.Program` (chứa bảng hằng số, hàm, và bytecode đã kiểm tra) và chạy phần khai báo cấp file để đăng ký cửa (`router.get`, `router.view`…) hoặc tạo cấu hình.
   - Mọi giá trị cấp file sau khi nạp đều được đóng băng (`vm.Freeze`).
2. **Mỗi Request một VM:**
   - Khi có request HTTP, engine lấy một `runtime.VM` sạch từ pool của site, gắn `Context` của request và chạy hàm handler đã biên dịch.
   - Không chia sẻ trạng thái biến đổi giữa hai request.
3. **Năng lượng, bộ nhớ, số request và thời hạn:**
   - Năng lượng (`MaxEnergy`) tính theo việc, không theo số lệnh: builtin và lệnh trên chuỗi dài, khoá dài, object lớn trả theo kích thước cái chúng đọc hay làm ra. Mặc định 10.000.000 mỗi lượt chạy (`core/runtime/limits.go`), đổi bằng `app.limits({ energy })`.
   - Bộ đếm byte: một lượt chạy làm ra tối đa 64 MiB mặc định, đổi bằng `app.limits({ memory })`; `app.limits({ host })` là pool byte chung của host.
   - Mỗi site chạy một số request cùng lúc (`app.limits({ requests })`); hết chỗ thì request chờ tới 1 s rồi là 503 (`host/gate.go`).
   - Thời hạn thực thi mặc định `30s` cho mỗi request (`host/serve.go`). Máy ảo kiểm context (huỷ, hết giờ) sau mỗi 64 lệnh bytecode mà không cần cấp phát bộ đếm giờ (`time.Timer`) riêng cho từng request.
   - `safe` không bắt các lần dừng này (hết năng lượng, hết bộ nhớ, huỷ, hết giờ).


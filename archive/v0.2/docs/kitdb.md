# KitDB

Đặc tả kiến trúc cơ sở dữ liệu nội tại KitDB của Kitwork: lưu trữ trên SQLite WAL, gộp ghi theo lô (Single-Writer Batching), lịch sử bất biến (`kitwork_history`), cột phân tích (`KCOL`), tìm kiếm toàn văn (`.search`) và cổng giao thức PostgreSQL (`pgwire`).

## 1. Bố cục lưu trữ trong `.database/`

Mỗi ứng dụng sở hữu một thư mục `.database/` hoàn toàn cô lập, không đi theo gói mã nguồn khi triển khai và không bao giờ được phục vụ qua HTTP:

```text
<app>/.database/
├── .catalog/
│   └── kitwork.db              Danh mục database, schema, tài khoản SCRAM và quyền GRANT của cổng
├── application/                Database mặc định của connection()
│   ├── kitwork.db              Schema "public" (SQLite WAL)
│   ├── kitwork.db.sql          Cấu trúc của file (bảng, chỉ mục), người ghi viết lại khi schema đổi
│   ├── kitwork.db.history/     Mốc neo (.base, .pages) để khôi phục một thời điểm
│   ├── kitwork.db.lock         Khoá của người ghi: một người ghi mỗi file
│   ├── kitwork.db.analytics    Bản lưu dạng cột (KCOL) cho cổng database
│   ├── kitwork.db.search       Chỉ mục toàn văn BM25 (KSNAP002) cho các cột .searchable()
│   └── billing.db              Schema "billing" của connection().schema("billing")
├── storage/                    Dữ liệu của datastore() trong kitwork/storage
├── disk/                       Dữ liệu đệm của disk() trong kitwork/storage
└── files/                      Tệp tin người dùng tải lên qua kitwork/file
```

Một thư mục chỉ có `.sql` mà không có file (bản clone của repo) được tạo file từ `.sql` khi người ghi mở lần đầu.

---

## 2. Cơ chế đọc/ghi trên SQLite (`kitdb/sqlite`)

1. **Một luồng ghi duy nhất, gộp theo lô (Single-Writer with Savepoint Batching):**
   - SQLite ở chế độ WAL cho phép nhiều luồng đọc song song cùng một luồng ghi.
   - Thay vì để các goroutine tranh chấp khoá ghi SQLite, KitDB gom tối đa **100 yêu cầu ghi** đang chờ vào một giao dịch vật lý (`BEGIN IMMEDIATE ... COMMIT`), chỉ cần một lần `fsync` xuống đĩa cho cả lô.
   - Mỗi yêu cầu ghi trong lô được bọc trong một `SAVEPOINT` riêng: nếu yêu cầu thứ 37 vi phạm ràng buộc `UNIQUE` hoặc trả lỗi qua `return error(...)`, chỉ `SAVEPOINT` thứ 37 bị `ROLLBACK TO`, 99 yêu cầu còn lại trong lô vẫn thành công bình thường. Việc trả thành công cho các yêu cầu còn lại chỉ diễn ra sau khi giao dịch vật lý commit thành công.
2. **Pool kết nối đọc song song:**
   - Các câu lệnh chỉ đọc (`list`, `first`, `count`, `exists`) chạy trực tiếp trên pool kết nối đọc (`mode=ro`, `query_only = ON`, `busy_timeout`) mà không chờ luồng ghi.
3. **Số tiền thập phân chính xác (`decimal(scale)`):**
   - Cột `decimal(scale)` được lưu dưới dạng số nguyên 64-bit nhân với $10^{\text{scale}}$ ngay trong SQLite. Nhờ đó các phép so sánh `<`, `>`, sắp xếp `ORDER BY`, chỉ mục B-Tree và phép cộng `SUM` đều chính xác tuyệt đối ở tốc độ số nguyên.

---

## 3. Lịch sử thay đổi bất biến (`kitwork_history`)

Lịch sử là của từng bảng, do database khai: `WITH (history = on)` sau bảng trong file `.sql`, hay `ALTER TABLE … SET (history = on)` qua cổng. Bảng tạo từ 07/11/2026 mặc định tắt; mọi bảng của file có từ trước thì bật. Mỗi commit có thay đổi của người ghi thêm một dòng vào `kitwork_history` **ngay trong cùng giao dịch SQLite**:

- Mỗi dòng là một commit: `rev`, `at`, `checksum`, `changes` (mỗi thay đổi: bảng, thao tác, rowid, ảnh trước/sau hay cột đã đổi, `by`). Câu của app ghi `by` rỗng; câu qua cổng ghi user của phiên.
- `checksum` nối chuỗi: SHA-256 của checksum trước, `rev`, `at` và `changes`. Sửa một dòng trên đĩa thì chuỗi đứt, `VerifyHistory` phát hiện.
- Host đặt số ngày giữ bằng `app.history({ days })` trong `application.work` (mặc định 30). Người ghi viết mốc neo (bản sao các trang của file, `.base` + `.pages` trong `<file>.history/`) khi có một ngày commit chưa nằm trong mốc nào, và bỏ những mốc, commit cũ mà không thời điểm nào trong số ngày giữ cần tới.

---

## 4. Lưu trữ cột phân tích (`KCOL`) và Tìm kiếm toàn văn (`.search`)

1. **Cột phân tích (`.analytics` — định dạng `KCOL`):**
   - Câu quét lớn của cổng database (`SUM`, `COUNT`, `GROUP BY` trên hàng triệu dòng) không quét từng dòng B-Tree của SQLite mà đọc từ tệp cột `.analytics` (khối 1.024 dòng, mỗi khối có thống kê `min`/`max`), tươi theo `rev` từ `kitwork_history`. Câu của app (`db.group(…)`, `sum(…)`) chưa đọc bản cột.
2. **Chỉ mục toàn văn (`.search` — định dạng `KSNAP002`):**
   - Các cột đánh dấu `.searchable()` được lập chỉ mục đảo với xếp hạng **BM25**, tách từ hỗ trợ tiếng Việt có dấu và không dấu, phục vụ điều kiện `posts.matches(q)` của app (`db.list((posts) => posts.matches(q))`) và `MATCH` của cổng: cùng một chỉ mục.

---

## 5. Cổng PostgreSQL Wire (`pgwire` + `gateway` + `catalog`)

Host có thể mở một cổng TCP nói giao thức PostgreSQL v3 (`pgwire`) cho phép các công cụ BI, DBeaver, `psql`, Grafana hoặc dịch vụ ngoài đọc và ghi trực tiếp vào KitDB:

```js
// application.work
app.database("./.database", {
  port: 5455,
  user: "kitdb",
  password: env.KITDB_PASSWORD,
});
```

`path` là thư mục `.database/` của host; cổng phục vụ các database của nó (`shopping`) và của mọi app (`<tên-miền>/<database>`). `port`, `password` bắt buộc; `user`, `password` là admin của mọi cụm; `host` mặc định `127.0.0.1`, ngoài máy chỉ qua TLS.

### Bảo mật và Khả năng của cổng `pgwire`

1. **Xác thực SCRAM-SHA-256:** Tài khoản và mật khẩu lưu dưới dạng verifier SCRAM-SHA-256 trong `.database/.catalog/kitwork.db`, không bao giờ lưu mật khẩu rõ.
2. **Phân quyền theo bảng (`GRANT SELECT, INSERT, UPDATE, DELETE ON TABLE ... TO user`):**
   - Tài khoản tạo bằng SQL qua cổng (`CREATE USER`, `GRANT`, `REVOKE`), lưu trong `.catalog/kitwork.db` của cụm.
   - Câu cắt ở dấu `;` đầu tiên (`statement.go`); quyền đọc từ chương trình `EXPLAIN` của câu (`authorize.go`): mọi bảng câu mở phải được cấp.
   - Câu ghi (`INSERT`, `UPDATE`, `DELETE`, từ 05/11/2026) không chạy trên kết nối của phiên mà đi qua người ghi KitDB của file, như câu của app: có lịch sử, `at()`, bản cột và chỉ mục toàn văn thấy nó.
3. **Chặn leo thang đặc quyền và chạm tệp hệ thống:**
   - Mọi lệnh `ATTACH`, `DETACH`, `PRAGMA`, `VACUUM`, `load_extension` từ kết nối không phải quản trị đều bị chặn cứng ngay ở tầng kiểm tra câu lệnh.
   - Kết nối SQLite của phiên luôn bật `PRAGMA query_only = ON` và gắn (`ATTACH`) các file schema (`.db`) ở chế độ chỉ đọc (`?mode=ro`); câu ghi đi qua người ghi.
4. **Tương thích công cụ PostgreSQL:**
   - Tự động dựng schema tạm `pg_catalog` và `information_schema` để `psql \dt`, DBeaver, TablePlus, Metabase liệt kê bảng và cột như một máy chủ PostgreSQL thật.
   - Hỗ trợ cú pháp tìm kiếm toàn văn mở rộng `SELECT ... FROM posts WHERE posts MATCH 'từ khoá'`.


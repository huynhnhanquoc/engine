# Schema

Đặc tả `"kitwork/schema"`: `struct`, các kiểu và luật của trường. Đây là chuẩn engine2 làm theo; lý do và lịch sử các quyết
định ở `docs/KITWORK_SCHEMA.md` của repo gốc, hai bản nói cùng một điều.

**Đích là database:** struct là cách khai bảng; `parse`, `pick`, form là đường dữ liệu vào bảng, `.secret()` và kiểu của dòng
là cửa ra. Khi hai cách hiểu va nhau, nghĩa của database thắng. Struct không phải bảng dùng chung bộ chữ.

**Trạng thái (16/11/2026):** `library/schema` có struct (mục 1–4) và form (mục 6, `library/schema/form.go`, `library/ui/form.go`);
`library/database` khai bảng, so với file database, `kitwork migrate`, và các câu lệnh (`list`, `first`, `count`, `insert`,
`update`, `upsert`, `delete`, `transaction`, `history`, `at`, …; [`database.md`](database.md)). Còn `UNSUPPORTED`: `.policy`,
`connect(…)`, chỉ mục một phần, `db.query(…)`.

## 1. Một chữ: `struct`

```js
// app/core/shop.work
import { struct, string, integer, choice } from "kitwork/schema";

const email = (value) => value.trim().toLowerCase();

export const signup = struct({
  email: string().transform(email).max(254).email(),
  name: string().transform((value) => value.trim()).min(1).max(100),
  age: integer().min(0).nullable(),
  plan: choice("free", "pro").default("free")
});
```

```js
// app/shop.example/dang-ky/router.work
import * as router from "kitwork/router";
import { signup } from "app/core/shop.work";

router.post((context) => {
  const { value, error } = safe(() => signup.parse(context.request.json()));
  if (error) {
    return context.status(400).json({ message: error.message });
  }
  return context.json(value);
});
```

- **Mọi hình dạng dữ liệu là `struct({ … })`:** dữ liệu của form, của request, đầu vào component, và bảng (mục 5).
- **Khai trong một file `.work` thường của app,** import từ đó ở mọi file. `import … from "kitwork/schema"` trong file có vai
  (`router.work`, `config.work`, `design.work`, `module.work`) là lỗi boot.
- **Bất biến:** mỗi bổ nghĩa trả một trường mới, trường cũ giữ nguyên; struct một file khai lúc boot được mọi request đọc cùng lúc.
- **Dùng lại một trường bằng `const`:** `const emailField = string().max(254).email();` rồi dùng ở hai struct. Không có `.shape`,
  không rải struct vào struct.
- **Tên trường không trùng phương thức của struct:** `parse`, `pick`, `form`, `toJSONSchema` là lỗi ở chỗ viết.
- `struct({})` (không trường nào), một giá trị không phải trường, là lỗi ở chỗ viết.

## 2. Kiểu

| Kiểu | Đọc ra | Ghi chú |
|---|---|---|
| `string()` | chữ | UTF-8 hợp lệ, không chứa byte NUL |
| `uuid()` | chữ | 8-4-4-4-12 chữ số hệ 16, giữ chữ thường; UUID đến từ nơi khác, engine không sinh |
| `integer()` | số | số nguyên trong ±(2^53−1) |
| `float()` | số | số thực 64 bit; không dùng cho tiền |
| `decimal(2)` | số | tối đa 2 chữ số sau dấu phẩy, 15 chữ số cả số; `decimal(12, 2)` là lỗi chỉ sang `decimal(2)` |
| `boolean()` | `true`, `false` | |
| `date()` | chữ `"YYYY-MM-DD"` | ngày lịch có thật |
| `timestamp()` | số mili giây từ 1970 UTC | một giá trị thời gian của engine đọc thành số mili giây |
| `choice("a", "b")` | chữ | một trong các chữ đã khai |
| `json()`, `json(struct)` | giá trị JSON | `json(struct)` được đọc theo struct |
| `array(kiểu)` | mảng | từng phần tử đọc theo kiểu |
| một struct | object | struct lồng |

Không có `number()`, `bigint()`, `text()`, `kitid()`, `table()`: mỗi chữ là lỗi nói cách viết bây giờ (mục 7).

## 3. Luật của trường

- `.nullable()`: nhận `null`; trường vắng cũng là `null`. Không có `.optional()`: dữ liệu từ ngoài và database chỉ cần một giá
  trị rỗng.
- `.default(giá_trị)`: chữ, số, `true`, `false` hay `null` viết thẳng, áp khi trường vắng (không thay `null`, `false`, `0`, chữ
  rỗng của `string()`); luật của trường phải nhận nó, kiểm khi struct được tạo.
- `.min(n)`, `.max(n)`: số là giá trị, `string()` là số ký tự (code point), `array()` là số phần tử; trên kiểu khác là lỗi.
- `.email()`: chỉ sau `string()`; đúng định nghĩa "valid email address" của HTML cho `<input type="email">`.
- `.transform(fn)`: hàm trả giá trị cùng kiểu, chạy trước mọi luật, nhiều hàm chạy theo thứ tự viết. Hàm trả kiểu khác là lỗi
  `TYPE`; hàm lỗi thì lỗi đó là lỗi của `parse`. Struct không phải bảng: server chạy hàm, hàm nào cũng được.

## 4. `parse` và `pick`

**`s.parse(dữ_liệu)`** trả một object mới gồm các trường của struct theo thứ tự khai, đã biến đổi và đã kiểm:

- Trường vắng: lấy `.default()`, hay `null` nếu `.nullable()`, không thì sai "bắt buộc". Chữ rỗng là trường vắng với mọi kiểu
  trừ `string()` (form gửi `""` cho ô bỏ trống).
- Chữ đọc theo kiểu: số viết thường như người gõ (`"28"`, `"-3"`, `"35000.50"`; không `1e3`, `0x10`, `012`, `+1`) là số;
  `"true"`, `"on"` là `true`, `"false"` là `false`. Ô checkbox không tích thì không gửi gì: viết `boolean().default(false)`.
- Khoá dữ liệu có mà struct không khai thì không vào kết quả.
- Sai thì trả một lỗi `INVALID_INPUT` nêu mọi trường sai (tối đa 20) và sai gì, không bao giờ nêu giá trị: "dữ liệu không hợp
  lệ: email: không phải địa chỉ email; name: cần ít nhất 1 ký tự". Trường lồng nêu đường dẫn: `address.city`, `tags[2]`.
- Lỗi mang `fields`, `safe` trao nó là `error.fields`: mỗi trường sai một `{ path, rule, message }` (tối đa 100), `path` là
  chỗ của trường (`["email"]`, `["tags", 0]`), `rule` là luật nó phạm: chữ người viết đã viết (`min`, `max`, `email`,
  `choice`), `required` cho trường vắng hay `null` mà trường cần, `type` cho giá trị không đúng kiểu (chữ chỗ số, ngày không
  phải `YYYY-MM-DD`, decimal quá số lẻ). Câu ghi cũng vậy: cột luật của nó từ chối, `unique` đã có, `foreign` trỏ tới dòng không
  có (khi câu ghi một khoá ngoại), nên một `safe` bọc cả `parse` và câu ghi đổ lỗi về đúng ô nhập.
- Lỗi `INVALID_INPUT` không bắt bằng `safe` mà thoát khỏi route thì câu trả lời là 400, lý do vào log (`router.md`).
- `parse` không đọc database và không giữ gì sau khi trả.

**`s.toJSONSchema()`** là cái `s.parse()` nhận, viết bằng JSON Schema (draft 2020-12), cho công cụ đọc nó: tài liệu API,
tham số công cụ của một agent. Mỗi trường kiểu của nó (thêm `null` khi `.nullable()`), luật theo chữ của JSON Schema
(`minLength`, `maximum`, `format: "email"`, `enum` của `choice(…)`), `default`, `.label()` là `title`, `.description()`;
`required` là trường `parse` cần. Không có cột engine điền và cột `.secret()`; object không đóng, vì `parse` bỏ qua khoá lạ.

**`.label("Họ tên")`, `.description("…")`:** trường như người đọc nó; `<label />`, `<hint />` rỗng của `<field>` lấy chúng,
`toJSONSchema()` cũng vậy. Một chữ viết thẳng, không rỗng.

**`s.pick("name", "age")`** trả một struct chỉ có các trường nêu, giữ luật của từng trường. Form sửa dùng `pick` để nói rõ nó
đổi được gì. Tên không có trong struct, hay nêu hai lần, là lỗi.

## 5. Bảng

Struct đặt vào `connection().tables({ … })` là bảng. Bảng dựa trên database:

1. Struct tả bảng của database, không tạo ra nó; khác nhau thì database thắng.
2. Chỉ `kitwork migrate` đưa struct vào database, người chạy đồng ý.
3. Database giữ kiểu, khoá, `NOT NULL`, `DEFAULT`, `UNIQUE`, `REFERENCES` cho mọi client (`columnDefinition`,
   `library/database/migrate.go`). `.min()`, `.max()`, `choice()`, `transform`, `.readonly()` do câu lệnh của engine giữ, không
   thành `CHECK`; client qua cổng chưa bị chúng giữ. `transform` của bảng chỉ gồm `trim()`, `toLowerCase()`, `toUpperCase()`
   (thứ khác là lỗi boot), ba phép chạy lại vẫn ra cùng giá trị, vì một giá trị có thể qua `transform` hai lần (`parse` rồi câu
   ghi); engine áp chúng lên mọi giá trị ghi vào cột và mọi giá trị so với cột trong điều kiện (`library/database/transform.go`).

Chữ của bảng: `id()`, `id("uuid")`, `id("kitwork")`, `foreign(bảng.cột)`, `.key()`, `.unique()`, `.index()`, `.searchable()`,
`.created()`, `.updated()`, `.cascade()`, `.readonly()`, `.secret()`.

**Khai bảng (lát 1, làm 06/11/2026; mọi file từ 17/11/2026):** host chạy mỗi file `.work` thường của app một lần lúc app boot, trước mọi lượt khác,
và giữ danh mục database của app (`library/database` `Catalog`). Một file site import file schema thì chạy lại nó lúc boot (mỗi
import được gói vào chương trình của file đó) và tìm lại đúng khai báo theo chỗ viết `.tables(…)`. Lỗi boot, chỉ đúng chỗ viết:

- struct có chữ của bảng mà không nằm trong `tables({ … })` nào;
- bảng không có khoá; struct có hai khoá; `id(…)` có `.default()`;
- khoá ngoại trỏ tới cột không phải khoá hay `.unique()` riêng, hay tới struct không có trong cùng `tables({ … })`;
  `foreign(() => bảng.cột)` được gọi lúc khai để biết đích;
- trường lồng hay `array()` không qua `json(…)`; cột trong nhóm `.unique("tên")` mà `.nullable()`;
- tên bảng `session`, tiền tố `kitwork_`, `_kitwork_`, `sqlite_`, hai bảng chỉ khác hoa thường, quá 63 ký tự;
- một struct làm hai bảng; một database và schema khai hai lần;
- `connection()` trong file có vai; `kitwork/router` trong file thường;
- `.policy`, `connect(…)`, chỉ mục một phần: `UNSUPPORTED` tới lát của chúng (`transform` của bảng, `.before`, `.after` đã có).

**Database là gốc (lát 2, nửa đầu, làm 06/11/2026):** sau khi khai, mỗi file database của app được đọc chỉ đọc (`mode=ro`) và
so với struct (`Catalog.Check`): `connection()` là `.database/application/kitwork.db`, một schema là file riêng cạnh nó. File
chưa có, file thiếu bảng, cột, ràng buộc unique, cột lưu khác cách (theo affinity của SQLite), số lẻ của decimal khác
(`kitwork_columns`), khoá khác: boot nói một dòng, câu lệnh lỗi theo nó (`Entry.Report`). Chỉ mục, kiểu trong `kitwork_columns`,
chỉ mục tìm kiếm thiếu: ghi chú. Cột file có mà struct không khai là của database. Đọc đúng cách lưu của engine cũ (`id()` serial,
`id("uuid")` uuid, `id("kitwork")` kitid, string text, boolean bool, timestamp datetime chữ ISO, decimal số nguyên × 10^số_lẻ).

**`kitwork migrate` (lát 2, nửa sau, làm 06/11/2026):** `engine2.Migrate(file, yes, input, output)` đọc mọi file thường của
mọi app như boot (`Host.Migrations`, chỉ đọc), lập kế hoạch cho mỗi file (`Catalog.Plans`), in phần thêm, phần ghi thêm, phần
khác để nguyên, chú ý khi có thể là đổi tên, và các câu PostgreSQL; hỏi [y/N] trừ khi `yes`; rồi áp từng kế hoạch trên người
ghi của KitDB (`Maintain`: đọc lại chỗ thiếu trong giao dịch, chạy câu đã dịch bằng `gateway.TranslateSchema`, sổ
`_kitwork_migrations`, kiểu trong `kitwork_columns`), người ghi viết `kitwork.db.sql`. Thêm cái file thiếu; cái khác database nói
và để nguyên. Điều khai báo không nói được thì hỏi, đánh số (`library/database/choices.go`): đổi tên cột hay bảng, bỏ cột cũ, viết
lại giá trị theo số lẻ mới. File một server đang giữ thì đổi qua cổng database của server khi `application.work` khai
`app.database` phục vụ file ấy, không thì từ chối. Luật của struct không thành `CHECK` (mục 5).

`.parse()` của một bảng bỏ qua cột engine sinh (`id(…)`, `.created()`, `.updated()`); `pick` một cột như vậy là lỗi.

- Khoá: `id()` số tăng dần (mặc định), `id("uuid")` UUIDv7 (insert mang sẵn id cũng được), `id("kitwork")` KitID, `.key()` khoá
  tự nhiên sau `string()`, `integer()`, `choice()`, `date()`. `uuid().key()` là lỗi chỉ sang `id("uuid")`; `uuid()` là cột giữ
  UUID của nơi khác.
- `.readonly()`: đặt một lần lúc insert, không bao giờ đổi; câu `update` ghi cột đó là lỗi lúc file boot, `upsert` chỉ ghi nó khi
  nó là đích so trùng, dòng mà `before` trả đổi nó là lỗi TYPE. Client qua cổng và form chưa bị nó giữ.
- `.secret()`: dòng đọc ra không có cột đó trừ khi `select` gọi tên nó; `history()` bỏ cột ấy khỏi `before`, `after`. Kiểm
  `context.json`, `context.bind`, SSE chưa có.
- Lỗi của database về đúng trường: trùng `.unique()`, dòng đích của `foreign()` không có, là lỗi `CONFLICT` cùng khuôn với lỗi
  của `parse` (`fields: [{ path: ["email"], rule: "unique" }]`), để form hiện dưới đúng ô nhập.
- engine2 đọc đúng các file `.database/` mà engine cũ viết: đổi engine không phải chuyển dữ liệu. Chữ của bảng trên struct không
nằm trong `tables({ … })` nào là lỗi boot (`Catalog.Declared`): một luật không ai giữ là một luật hỏng (hỏng thì đóng,
`engine.md` mục 1).

## 6. Giao diện

- `s.form()`, `s.form(dữ_liệu)`, `s.form(dữ_liệu, lỗi)` gắn xuống trang bằng `context.view({ form })` hay `context.bind({ form })` ([`view.md`](view.md)): `dữ_liệu` là chữ người
  dùng gõ (`context.request.form()`) hay một dòng đọc ra, `lỗi` là `error` của `safe`. `form.email` là giá trị của trường;
  form trống thì là `.default()` của nó.
- Ô gắn trường là `<input>`, `<select>`, `<textarea>` có `value="{{ form.email }}"`. Engine ghi `name`, `type` theo kiểu
  (`email`, `number`, `date`, `checkbox`), `required` (không `.nullable()`, không `.default()`), `minlength`, `maxlength`,
  `min`, `max`, `step` (`decimal(2)` là `0.01`, `float()` là `any`), giá trị; `aria-invalid` khi form giữ lỗi của trường.
  Checkbox có `value="true"` và `checked` theo giá trị; ô `type="password"` không bao giờ ghi lại giá trị.
- `choice(…)` là `<select>`: trang không viết `<option>` thì lựa chọn là các chữ của nó (thêm lựa chọn rỗng đầu khi trường
  nhận `null` hay chưa có giá trị); trang viết `<option>` (từ dữ liệu, như dòng của khoá ngoại) thì engine đánh `selected` cái
  là giá trị, và với `choice(…)` một `<option>` không là chữ của nó là lỗi.
- Một nguồn: trang viết lại một thuộc tính luật đã cho là lỗi, trừ khi chặt hơn (`maxlength` dưới `.max()`, `required` trên
  trường `.nullable()`), khi đó lấy cái trang viết. `name` luôn của struct, `type` cũng vậy khi kiểu cho nó; chữ thường không
  cho `type`, nên trang viết `password`, `tel`, `url`.
- Trong `<field>`: `<label />`, `<hint />` rỗng là `.label()`, `.description()` (trường không có là lỗi), `<error />` rỗng là
  lỗi của trường, ẩn khi không có; ô mô tả bằng nó (`aria-describedby`) khi có lỗi.
- Không có ô: cột engine điền, cột `.secret()`, trường nhiều giá trị (`json`, `array`, struct), `timestamp()` (số mili giây).
- `context.attributes()` của component không nhận đối số (`library/router/bind.go`); kiểm đầu vào component bằng struct chưa có.
- Trình duyệt chỉ nhận luật khai báo, dưới dạng thuộc tính HTML; không hàm nào của struct xuống trình duyệt.

## 7. Cách viết cũ

| Viết | Lỗi nói |
|---|---|
| `table({ … })` | viết `struct({ … })`; struct trong `connection().tables({ … })` là bảng |
| `text()` | viết `string()` |
| `number()` | số đếm dùng `integer()`, tiền dùng `decimal(2)`, số đo dùng `float()` |
| `bigint()` | `.work` không có BigInt; số đếm `integer()`, mã số lớn của hệ khác `string()` |
| `kitid()` | khoá KitID là `id("kitwork")`, KitID của hệ khác là `string()` |
| `.normalize(fn)` | viết `.transform(fn)` |

## 8. Trong engine

- `library/schema`: `Schema` (cái `import` đọc), `Field` (một trường), `Struct`; mọi giá trị bất biến, `Freeze()` không làm gì.
- **Hàm của struct chạy trong request:** một hàm file khai lúc boot chỉ chạy trong request khi được gọi hay được trao cho một
  method (`core/runtime/freeze.go`); hàm `transform` struct giữ thì không. `Struct` là một `value.Keeper`: trước mỗi lời gọi
  method của nó, VM trao struct qua (`HandOver`) và lời gọi chạy trên một bản sao có các hàm chạy trên VM của request
  (`core/runtime/boundary.go`, `nativeMethod`). Struct gốc không đổi.
- Câu lỗi gọi một struct của thư viện bằng tên kiểu của nó (`napas`, `request`, `struct`), `value.Value.Kind`.
- `host/site.go`: lỗi `INVALID_INPUT` thoát khỏi route là 400.

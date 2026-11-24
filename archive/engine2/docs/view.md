# View

Đặc tả cửa màn hình `router.view` và cơ chế gắn dữ liệu, trạng thái, việc (`context.bind`) cho giao diện `.ui`.

## 1. Tổng quan

`router.view` vẽ trang giao diện `content.ui` trong khung `view.ui` gần nhất, có trạng thái và việc. Trang không trạng thái còn hai
đường: `context.view({ ... })` của cửa khác (xem [`router.md`](router.md) mục 6), và thư mục chỉ có `content.ui`, không cửa, trả
`GET` bằng trang của nó:

```js
// san-pham/router.work
import * as router from "kitwork/router";
import { db } from "app/core/shop.work";

router.view((context) => {
  const products = db.list((products) => products.active);
  var query = "";
  var editingId = null;

  const startEdit = (id) => {
    editingId = id;
  };

  const remove = (id) => {
    db.where((products) => products.id === id).delete();
  };

  return context
    .title("Danh sách sản phẩm")
    .description("Quản lý sản phẩm trong cửa hàng")
    .sync({ products, remove })
    .bind({
      query,
      editingId,
      startEdit,
    });
});
```

```html
<!-- san-pham/content.ui -->
<content gap="4" padding="6">
  <input value="{{ query }}" placeholder="Tìm sản phẩm..." />
  <list>
    <item for="product of products" key="product.id" show="product.name.includes(query)">
      <text>{{ product.name }}</text>
      <button click="startEdit(product.id)">Sửa</button>
      <button click="remove(product.id)" disabled="{{ remove.pending }}">Xoá</button>
      <text show="remove.error" color="danger">{{ remove.error.message }}</text>
    </item>
  </list>
</content>
```

---

## 2. Luật của dòng `return`: `context.bind` và `context.sync(...).bind(...)`

1. **Một cửa màn hình mỗi thư mục:**
   - `router.view(fn)` đứng ở cấp cao nhất của `router.work`, tối đa một lần mỗi thư mục.
   - Thư mục có `router.view` bắt buộc phải có `content.ui`; thiếu `content.ui` là lỗi lúc boot.
   - Không được khai báo `router.get` cùng thư mục với `router.view` (cả hai chiếm cửa `GET`, lỗi boot). `router.proxy` chưa phục vụ: bỏ qua kèm một dòng boot.
   - Các cửa `post`, `put`, `patch`, `delete` cùng thư mục với `router.view` được phép trả về `context.bind({ ... })` (hoặc `context.sync({ ... }).bind({ ... })`, hoặc `context.redirect(...)`) để hỗ trợ form HTML gửi không cần JavaScript.
2. **Hai dạng câu trả lời vẽ trang trên dòng `return`:**
   - `return context.bind({ ... })`
   - `return context.sync({ ... }).bind({ ... })` (`.bind({ ... })` luôn đứng cuối chuỗi, sau `.title(...)`, `.description(...)`, `.meta(...)`, `.header(...)`, `.cookie(...)`, `.sync({ ... })`).
   - Ngoài ra `router.view` còn trả về được `context.redirect(url)`, `context.notFound()` (404), `context.error(message?)` (500).
   - Các câu trả lời dữ liệu thô (`context.json`, `context.text`, `context.file`) trong `router.view` là lỗi biên dịch.
3. **Phân vai giữa `.sync({ ... })` và `.bind({ ... })` (Luật dòng `return`):**
   - **Cái gì xuống trình duyệt thì phải có tên trên dòng `return`:** Không gửi ngầm dữ liệu đọc từ database/storage, không gửi ngầm `context.attributes()` hay `context.*` nếu không có tên trên dòng `return`.
   - **`.sync({ ... })` (Phía Server):** Chứa dữ liệu do server tính và giữ sống theo bản ghi của tab (như `products`), cùng các việc chạy ở server (`remove`). Khi dữ liệu nguồn đổi hoặc sau khi một việc chạy xong, server tính lại và chỉ gửi phần thay đổi (diff theo từng dòng `tên#khoá` hoặc theo tên) xuống trình duyệt.
   - **`.bind({ ... })` (Phía Trình duyệt):** Chứa trạng thái sống `var` (`query`, `editingId`), các biểu thức/hàm tính toán và hàm chạy ở trình duyệt (`startEdit`).
   - Đối số của cả `sync` và `bind` đều phải là object literal viết trực tiếp tại chỗ (`{ ... }`), không dùng spread `{ ...obj }` hay biến trung gian.
   - Nếu hàm chạy ở trình duyệt đọc dữ liệu từ database/storage/đầu vào mà tên đó không có mặt trong `sync` hoặc `bind`, trình biên dịch báo lỗi ngay lúc boot.

---

## 3. Phân loại tên trong `router.view`

| Khai báo trong `router.view` | Vai trò | Hành vi |
|---|---|---|
| `const products = db.list(...)` | **Dữ liệu lúc dựng** | Tính ở server khi mở trang và khi vẽ lại sau một việc; in sẵn vào giao diện |
| `var query = ""` | **Trạng thái màn hình** | Khởi tạo lúc dựng; khi thay đổi ở client hoặc qua bản vá từ server, giao diện tự động cập nhật |
| `const startEdit = (id) => { ... }` (chỉ đụng `var`) | **Việc ở client** | Được biên dịch ra JS ở trình duyệt; chạy tức thì không qua mạng |
| `const remove = (id) => { ... }` (đụng `db`, `http`, `file`, `context`…) | **Việc ở server** | Thân hàm giữ ở server; ở client sinh thành lời gọi qua đường ống về chính URL của trang |

- Người viết **không** đánh dấu `"use server"` hay `"use client"`. Trình biên dịch phân tích tĩnh cây lời gọi trong thân hàm để tự động phân loại.
- Hàm phụ ở server (như `const checkStock = () => db.first(...)`) không đưa vào `bind` thì client không thể gọi trực tiếp, nhưng các việc trong `bind` vẫn gọi nó bình thường ở server.

---

## 4. Vòng đời của một Việc chạy ở Server

Khi người dùng kích hoạt một việc chạy ở server (ví dụ `click="remove(product.id)"`):

1. **Đang chờ (`.pending`):**
   - `remove.pending` bật thành `true` ngay lập tức cho đúng dòng/instance vừa bấm (trong vòng lặp `for`, chỉ nút của dòng đó bị khoá; đọc ngoài vòng lặp thì `remove.pending` là `true` khi có bất kỳ lời gọi `remove` nào đang chạy).
2. **Kiểm tra tại Server:**
   - Request đi qua kiểm tra nguồn gốc (`Origin`), giới hạn tần suất (`router.limit`) và toàn bộ chuỗi `router.guard` của nhánh và của `router.view`.
   - Server chỉ chấp nhận tên việc có trong `bind` của chính trang đó (hoặc của component mà trang đó có vẽ). Mọi tên lạ hoặc hàm chỉ chạy ở client đều nhận `404`.
3. **Thực thi và Chạy lại để cập nhật giao diện:**
   - Việc chạy với `context` của request hiện tại; các biến `var` mà việc đọc được gửi lên từ client (server kiểm tra kiểu và kích thước, không bao giờ tin mù quáng), còn các `const` của trang được lấy lại ở server.
   - Sau khi việc hoàn tất (và không trả về `context.redirect(...)`), engine chạy lại hàm của `router.view` để lấy dữ liệu `const` mới nhất cùng các `var` mà việc vừa gán, rồi gửi bản vá + dữ liệu mới về để morph giao diện mà không làm mất các `var` khác người dùng đang gõ dở trên màn hình.
4. **Đặt Cookie hoặc Chuyển hướng trong Việc:**
   - Việc gọi được `context.cookie(...)` (ví dụ đăng nhập/đăng xuất) và `return context.redirect("/login")`.
   - Nếu việc đổi cookie mà không chuyển hướng, lần chạy lại để vẽ trang sẽ đi qua `router.guard` với cookie mới; nếu guard chuyển hướng về `/login`, trang tự động chuyển sang `/login`.
5. **Khi có lỗi (`.error`):**
   - Nếu guard chặn (`403`), quá giới hạn (`429`), hoặc việc trả lỗi qua `return error("...")` mà chưa được `safe` xử lý (`500` mặc định), trang không bị xoá trắng mà gán `{ code, message }` vào `remove.error` của đúng dòng vừa gọi. Đây là lỗi công khai trên đường ống; không gửi stack hay thông tin nội bộ (xem [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe)).
   - Khi gọi lại lần sau, `remove.error` tự động trở về `null`.

---

## 5. Phối hợp với Component và Hộp thoại xác nhận

Khi cần hỏi xác nhận trước khi chạy một việc ở server, truyền việc vào callback của component hộp thoại (`$dialog` lấy từ `component.alias()` hoặc `as="$dialog"`):

```html
<button click="$dialog.open({
  title: 'Xoá sản phẩm ' + product.name + '?',
  confirm: () => remove(product.id)
})">
  Xoá
</button>
```

- `$dialog.open` chạy hoàn toàn ở client.
- Khi người dùng bấm xác nhận, callback `confirm` mới gọi `remove(product.id)` qua đường ống kèm đầy đủ trạng thái `remove.pending` và `remove.error`.

---

## Trạng thái triển khai (16/11/2026, lát 1, 2 và 4)

engine2 làm lát 1 (trang vẽ ở server) và lát 2 (việc ở server qua đường ống, trang chạy trong trình duyệt): `library/router/bind.go`,
`library/router/bridge.go`, `host/page.go`, `host/bridge.go`, `library/ui/bridge.go`, `library/ui/kernel/call.js`.

- `router.view(fn)`: GET và HEAD chạy `fn`. Trả `context.bind({ … })` hay `context.sync({ … }).bind({ … })` thì vẽ `content.ui`
  trong `view.ui` gần nhất với các tên của cả hai; `title`, `description`, `header`, `cookie` viết trước đi theo; trang mang
  `Cache-Control: no-store` trừ khi handler tự đặt. Trả `redirect`, `notFound`, `error` như mọi cửa. Trả `json`, `text`,
  `html`, `view` hay câu trả lời khác là 500, lý do vào log: mục 2 nói lỗi biên dịch, engine2 báo lúc chạy.
- Cửa `post`, `put`, `patch`, `delete` cùng thư mục với `router.view` trả `context.bind({ … })` thì vẽ cùng trang (form không cần
  JavaScript). `bind` ở cửa khác (`router.get`, cửa của thư mục không có `router.view`) là 500.
- `sync({ … })` là chỉnh sửa đứng trước `bind`, một lần; đối số là object; một tên có ở cả hai là lỗi. Chưa kiểm lúc biên
  dịch "object viết tại chỗ".
- Thư mục có `router.view` mà không có `content.ui` là lỗi boot. Từ 15/11, `context.view(…)` viết trong hàm của `router.get`,
  `post`, `put`, `patch`, `delete` ở thư mục không có `content.ui` cũng là lỗi boot (`core/compiler/door.go`).
- `var` (language.md mục 2) là từ khoá: ở cấp cao nhất của hàm `router.view` hay `component.define` (viết tại chỗ hay một `const`
  cấp file truyền vào, kể cả sau `router.guard(…)`), compiler viết nó thành `let x = context["var#"]("x", giá trị, () => x)`;
  ở cấp cao nhất của file, trong hàm khác, trong khối, không có tham số `context` hay viết kiểu tách là lỗi biên dịch. `let`
  ở cấp cao nhất của hàm view chưa là lỗi.
- **Trang chạy trong trình duyệt:** `bind` (hay `sync`) có một hàm hay một `var` thì trang là instance `page` của kernel. Chỗ
  đọc tên của trang mang thuộc tính kernel (`data-kitwork-bind`, `-class`, `-event`); danh sách đọc tên của trang vẽ theo
  dấu `kitwork-for-start`, mẫu dòng và key (không `key` thì theo chỉ số); `if` đọc tên của trang vẽ như `show`; `submit` của
  form tự thêm `prevent`; thuộc tính boolean (`disabled`, `checked`…) in khi đúng, bỏ khi sai. Dữ liệu của trang viết một lần
  ở cuối `<content />`: `<script type="application/json" data-kitwork-page>{ "page": {…}, "jobs": { "page": […] }, "vars":
  { "page": […] } }`, `<`, `>`, `&` viết bằng mã thoát. Trang không có hàm và `var` in ra như cũ. Mọi biểu thức kernel chạy trên
  trang (giá trị của `data-kitwork-bind`, `-class`, `-style`, `-if`, `-for`, `-event`) được server viết thành JavaScript vào một file
  `/_kitwork/page.<hash>.js`, nạp sau kernel (`library/ui/program.go`, 15/11); kernel không tự đọc biểu thức.
- **Việc qua đường ống (mục 4):** mọi hàm của `bind` và `sync` là việc ở server (lát 3 mới chia hàm chạy ở trình duyệt).
  `POST <URL trang>?kitwork-call=<việc>` với `{ seq, args, page: { var… } }`, thân tối đa 1 MiB. Lời gọi qua kiểm nguồn,
  các luật của các thư mục áp cho GET hay POST, rồi luật đứng trước `router.view` (giới hạn đếm cùng trang); mỗi trang một
  lời gọi một lúc. Hàm của trang chạy với `var` gửi lên (cùng loại với giá trị đầu, hay `null`; khác là 400), việc chạy,
  `var` đọc lại, guard chạy lại với cookie việc đặt như trình duyệt sẽ gửi, hàm chạy lại; câu trả lời chỉ gồm tên đã đổi
  `{ seq, page: { … } }` (tên biến mất là `null`). Việc trả `redirect` là `{ seq, redirect }`; `error`, `notFound` là
  `{ seq, error: { code, message } }` với chữ của site; lỗi không ai bắt là 500 (400 với `INVALID_INPUT`), chữ là chữ của
  status, lý do vào log; trả câu khác là 500. Guard từ chối là lỗi theo status của nó (chữ nếu là text, cookie và
  `Retry-After` đi theo), chuyển hướng là `{ seq, redirect }`.
- **call.js:** đặt dữ liệu vào trạng thái gốc của kernel, mỗi việc thành hàm cùng tên có `.pending` (theo dòng khi đọc trong
  dòng) và `.error`; một lời gọi một lúc theo thứ tự; lỗi thì ô người dùng vừa đổi về theo dữ liệu; Drive đổi trang thì bỏ
  lời gọi cũ và đọc khối của trang mới.
- **Dữ liệu sống của `sync` (lát 4, `host/tab.go`):** trang (hay instance) có tên dữ liệu trong `sync` được vẽ cho một tab: khối
  dữ liệu mang `"tab"`, server nhớ mã băm từng tên sống tab đang giữ, theo instance, và các khoá `kitwork/storage` lần chạy đã
  đọc (`get`, `exists`, `ttl`; runtime trao context của lần chạy cho handle, `value.Attender`). Tab mở `GET
  /_kitwork/changes?tab=…` (SSE, chỉ báo `changed`, nhịp tim như `context.sse`); một lần `set`, `delete` có xoá, `clear` có dọn
  vào khoá (hay nhánh) tab đã đọc thì báo tab. Tab hỏi lại `POST <trang>?kitwork-refresh=1` `{ seq, tab, page: { var… }, <chỗ>:
  { var… } }`, qua guard của trang (không qua giới hạn), trong hàng chờ một lời gọi một lúc; server chạy lại trang và các
  instance với `var` của chúng, trả các tên sống khác với bản ghi, theo instance (trang trước), rồi bản ghi nhận. Lời gọi một
  việc mang `tab` so tên sống với bản ghi (không với lần chạy trước việc), nên mang cả thay đổi tab đã lỡ, rồi bản ghi nhận. Tab ẩn đóng kênh; thấy lại, hay kết nối lại, hỏi một lần. Tên trong `bind` không sống.
  Mỗi app nhớ `StreamLimit` × 10 tab (quên tab không nghe lâu không dùng nhất), mở tối đa `StreamLimit` kênh (503). Tab gây ra
  lần ghi cũng được báo (lần hỏi lại thường không có gì). Bản ghi trong bộ nhớ một tiến trình; mất thì lần hỏi sau gửi nguyên
  các tên sống. Ghi `kitwork/database` chưa báo tab.
- Chưa có: hàm chạy ở trình duyệt, phân loại bằng biên dịch (lát 3); gửi theo dòng `tên#khoá`; gộp phím khi gõ; các lỗi biên dịch
  của mục 2; `for` lồng trong dòng chưa thử; báo tab khi ghi database; chỉ chạy lại instance bị đánh dấu; nhiều tiến trình.

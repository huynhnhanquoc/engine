# Component

Đặc tả định nghĩa và sử dụng Component trong `.work` và `.ui` qua thư viện `"kitwork/component"`.

## 1. Tổng quan

Component là một khối giao diện có trạng thái (`var`) và hành vi riêng, được gọi bằng một thẻ có dấu gạch ngang (như `<shop-cart>`):

```js
// core/cart.work: một file .work thường bất kỳ của app
import * as component from "kitwork/component";
import { db } from "app/core/shop.work";

const shopCart = (context) => {
  const { shippingFee = 15000 } = context.attributes(); // Đầu vào từ thuộc tính trên thẻ
  var items = [];                                       // Trạng thái của từng instance
  var done = false;

  const add = (product) => {                            // Việc chạy ở server (chạy ở trình duyệt: lát 3, chưa làm)
    items.push({ id: product.id, name: product.name, quantity: 1 });
    done = false;
  };

  const remove = (id) => {                              // Việc chạy ở server (chạy ở trình duyệt: lát 3, chưa làm)
    items = items.filter((item) => item.id !== id);
  };

  const checkout = () => {                              // Đụng db → việc chạy ở server
    db.insert((orders) => ({ items, shippingFee }));
    items = [];
    done = true;
  };

  return context.sync({ checkout }).bind({ items, done, add, remove });
};

component.define("shop-cart", shopCart);
```

```html
<!-- core/shop-cart.ui, cạnh file khai: giao diện dùng lại có gốc là chính thẻ component -->
<shop-cart flex="column" gap="3">
  <list>
    <item for="line of items; key: line.id">
      <text>{{ line.name }} × {{ line.quantity }}</text>
      <button click="remove(line.id)">Bỏ</button>
    </item>
    <empty show="items.length == 0">Giỏ hàng trống.</empty>
  </list>
  <button click="checkout()" disabled="{{ checkout.pending || items.length == 0 }}">
    Đặt hàng
  </button>
  <text show="checkout.error" color="danger">{{ checkout.error.message }}</text>
  <text show="done" color="success">Đã đặt hàng thành công!</text>
</shop-cart>
```

```html
<!-- Gọi trong content.ui -->
<shop-cart shippingFee="{{ 20000 }}" />
<!-- as="$cart" trên component của site hôm nay bị bỏ qua: $cart.add(…) chưa gọi được -->

```

---

## 2. Chỗ đặt và Quy tắc đặt tên

1. **Tên thẻ có gạch ngang (`kebab-case`):**
   - Tên component của site/app phải có ít nhất một dấu gạch ngang ở giữa (`"shop-cart"`, `"account-menu"`).
   - Tên không có gạch ngang (`"dropdown"`, `"dialog"`, `"tabs"`, `"toast"`, `"carousel"`) dành riêng cho các component tích hợp sẵn của Kitwork.
2. **Khai ở cấp file của một file `.work` thường bất kỳ của app** (từ 17/11/2026, lát 7 của `KITWORK_MODULE_WORK.md` ở repo gốc; tới đó là `components.work` ở gốc site và `.shared/components/`):
   - Component là của cả app: mọi site của app vẽ được nó. Mỗi tên một chỗ khai trong app; hai chỗ là lỗi boot nêu cả hai (`component.define("shop-cart", …) at shop.test/components.work:2:10 again: it is defined at core/a.work:2:10`). Cùng lời khai chạy lại vì một file import file ấy (kể cả `router.work` của trang) thì được tìm lại theo chỗ viết, không tính là lần hai.
   - Gọi `component.define` trong `router.work`, `config.work`, `design.work`, `module.work` hay lúc request là lỗi; các việc của component chạy dưới guard của trang đang vẽ nó.
   - File trong thư mục gạch dưới (bản nháp) hay thư mục chấm không được boot đọc: component khai ở đó không có, và một trang import file ấy là lỗi boot nêu chỗ dời (`core/`).
   - `components.work` không còn vai riêng: là một file thường như mọi file. `.shared/components/` là lỗi boot nêu chỗ mới.
   - `kitwork check` in mỗi component và chỗ khai (`component shop-cart core/cart.work:4:10`).
3. **Một file `.work` định nghĩa được nhiều component:**
   - Có thể truyền hàm trực tiếp vào `component.define("theme-switch", (context) => { ... })` hoặc khai báo `const themeSwitch = (context) => { ... }` trước rồi gọi `component.define("theme-switch", themeSwitch)` ở cuối file.

---

## 3. Đầu vào (`context.attributes`) và Đầu ra (`context.bind`)

### 3.1. Đọc đầu vào bằng `context.attributes()`

- Đọc bằng một dòng destructure ở cấp cao nhất của hàm component (chưa kiểm):
  ```js
  const { orderId, compact = false } = context.attributes();
  ```
- Chỗ gọi `<order-detail orderId="{{ item.id }}" />` thiếu một đầu vào thì nó là `undefined`; kiểm lúc biên dịch đầu vào bắt buộc chưa có.
- `for`, `if`, `key`, `as`, `show`, sự kiện và thuộc tính trình bày (`padding`, `gap`, `flex`…) không vào `context.attributes()`; `source` trên component là lỗi.
- Ở server, khi một việc của component được gọi qua đường ống, các giá trị đầu vào được lấy lại bằng cách chạy lại trang ở server — **không bao giờ tin giá trị đầu vào do client gửi lên**.

### 3.2. Xuất phạm vi bằng `return context.bind({ ... })` hoặc `return context.sync({ ... }).bind({ ... })`

- Mọi hàm component bắt buộc kết thúc bằng `return context.bind({ ... })` hoặc `return context.sync({ ... }).bind({ ... })` (`.bind({ ... })` luôn đứng cuối) với các object literal viết tại chỗ.
- **Chỉ những tên có mặt trên dòng `return` (`sync` và `bind`) mới ra ngoài tới giao diện của component và lên cầu nối:**
  - **`.sync({ ... })`:** giữ dữ liệu do server tính/giữ sống và các việc (`checkout`).
  - **`.bind({ ... })`:** giữ trạng thái `var` (`items`, `done`) cùng dữ liệu và các hàm (`add`, `remove`).
  - Mọi hàm trong `bind`/`sync` là việc chạy ở server qua đường ống (hàm chạy ở trình duyệt: lát 3, chưa làm).
  - Đầu vào từ `context.attributes()` không được tự động gửi xuống trình duyệt; nếu giao diện cần đọc một đầu vào thì phải đặt tên nó lên dòng `return`.

---

## 4. Giao diện của Component

Mỗi thẻ `<shop-cart>` dựng đúng **một** phần tử `box`. Có hai cách viết giao diện:

1. **Giao diện viết tại chỗ:** Viết nội dung trực tiếp bên trong thẻ `<shop-cart>...</shop-cart>` tại trang sử dụng.
2. **Giao diện dùng lại (File `<tên>.ui`):** Tạo file `shop-cart.ui` cạnh file `.work` khai component (`core/shop-cart.ui` cạnh `core/cart.work`), có thẻ gốc là chính `<shop-cart>...</shop-cart>`. Một site muốn giao diện riêng thì đặt `shop-cart.ui` ở thư mục của trang hay một thư mục cha tới gốc site: file gần trang nhất thắng, rồi mới tới file cạnh chỗ khai. Một mảnh không có logic mà nhiều site dùng chung thì khai thành component (`component.define("site-footer", (context) => context.bind({}))`) để có chỗ ở. Khi đó mọi nơi chỉ cần gọi thẻ rỗng `<shop-cart />`.

---

## 5. Alias (`as="$name"` và `component.alias()`)

Khi một phần tử bên ngoài hoặc một hàm trong `.work` cần gọi phương thức/đọc trạng thái của một instance component trên trang, gắn `as="$tên"` lên thẻ của component đó:

```html
<!-- Trong view.ui hoặc overlay.ui -->
<dialog as="$dialog" />
<toast as="$toast" />
```

- **Trong `.ui`:** Gọi trực tiếp qua tên `$dialog.open(...)`, `$toast.success(...)`. `as`/`$tên` hôm nay chỉ cho component của Kitwork; `as` trên component của site bị bỏ qua.
- **Trong `.work` (`router.view` hoặc `component.define`):** `component.alias()` chưa có (lỗi `UNSUPPORTED`):
  ```js
  import * as component from "kitwork/component";

  const { $toast } = component.alias(); // chưa có
  ```

---

## Trạng thái triển khai (16/11/2026, lát 1, 2 và 4; 17/11/2026 tự khai)

engine2 làm lát 1 (component vẽ ở server) và lát 2 (việc của component qua đường ống): `library/component/define.go`,
`library/ui/sitecomponent.go`, `host/sitecomponent.go`, `host/bridge.go`.

- `component.define(name, fn)` chạy lúc boot, ở cấp file của mọi file `.work` thường của app, trong lượt nạp việc của app
  (cùng lượt với job và lịch, `host/background.go`); lượt khai bảng bỏ qua nó. Tên là chữ thường, số và các từ nối bằng một
  gạch ngang, có ít nhất một gạch, bắt đầu bằng chữ. Mỗi tên một chỗ khai trong app, hai chỗ là lỗi boot. `define` trong
  file có vai hay lúc request, `kitwork/router` trong một file thường là lỗi boot.
- Mỗi lần trang vẽ thẻ, hàm chạy ở server với năng lượng của một request. `context.attributes()` trả các thuộc tính viết trên thẻ,
  đọc ở chỗ gọi, trừ `for`, `if`, `key`, `as`, `show`, sự kiện và thuộc tính trình bày (`gap`, `padding`…); ngoài component là
  lỗi. `context.request` là của trang; `context.cookie(tên)` đọc cookie (16/11). Hàm phải trả `context.bind({ … })` (hay
  `sync({ … }).bind({ … })`); đặt cookie (`context.cookie(tên, giá trị)`), `status`, `header`, `title`, `description` hay chữ
  khác của trang trong hàm component là lỗi (việc của nó đặt được cookie).
- Giao diện là nội dung viết trong thẻ, hay file `<tên>.ui` (tìm như mảnh layout) có gốc là chính thẻ. Nó vẽ thành một `box`
  mang thuộc tính trình bày của gốc và của chỗ gọi; một thuộc tính viết ở cả hai chỗ là lỗi. `show` trên thẻ đọc ở chỗ gọi.
  Bên trong, tên của `bind` và `sync` đứng trước tên ở chỗ gọi; tên trùng thì có cảnh báo.
- Mảnh layout không có `define` mà có gốc là chính thẻ của nó thì gốc vẽ thành `box`, không bị coi là tự gọi mình.
- **Instance ở trình duyệt:** component có hàm hay `var` trong `bind` là một instance của kernel, tên là chỗ gọi: `c` và mã băm
  FNV của file, dòng, cột của thẻ, thêm `-2`, `-3` cho instance thứ hai trở đi của cùng chỗ (một dòng của danh sách). `box`
  mang `data-kitwork-component="<tên>" data-kitwork-at="<chỗ>"`, tên của nó là của kernel; khối dữ liệu của trang có dữ liệu,
  việc và `var` của từng instance dưới chỗ của nó. Component không có hàm và `var` in ra như cũ. Trang thường (chỉ
  `content.ui`) có instance cũng nạp kernel và call.js.
- **Việc của component:** `POST <URL trang>?kitwork-call=<tên>.<việc>` với `{ seq, args, at, page: {…}, <chỗ>: { var… } }`, trên
  trang của `router.view` hay trang chỉ có `content.ui`. Đầu vào không bao giờ lấy từ trình duyệt: server vẽ lại trang, instance ở
  đúng chỗ (đúng tên thẻ) chạy với `var` gửi lên, việc chạy trên những gì nó `bind`, `var` đọc lại, guard chạy lại với cookie
  mới, trang vẽ lại; câu trả lời là phần đã đổi của trang và của instance, mỗi phần dưới chỗ của nó. Chỗ không còn trên trang,
  thẻ site không khai, việc không có là 404.
- **Tên sống:** component có tên dữ liệu trong `sync` là instance (cả khi không có việc hay `var`) và giữ sống như trang
  (`view.md`, trạng thái lát 4): lần hỏi lại của tab vẽ lại trang để chạy instance với `var` nó giữ, phần đổi về dưới chỗ của nó.
- Chưa có: `component.alias()` (`UNSUPPORTED`), `as` (bỏ qua), `source` trên component (lỗi); `for` của danh sách chạy ở trình duyệt
  đặt thẳng trên thẻ component (lỗi, bọc trong `<item>`); kiểm lúc biên dịch đầu vào bắt buộc ở từng chỗ gọi; hàm chạy ở
  trình duyệt (lát 3).

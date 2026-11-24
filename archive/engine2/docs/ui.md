# UI

Đặc tả ngôn ngữ giao diện `.ui` của Kitwork.

## 1. Bản chất của `.ui`

`.ui` là ngôn ngữ đánh dấu mô tả ý định giao diện (cấu trúc, ngữ nghĩa, bố cục, dữ liệu và tương tác), độc lập với đích hiển thị (HTML/DOM trên web, WebView hoặc native renderer trên desktop/mobile):

- **Chỉ có thẻ, tên thẻ nói rõ chủ sở hữu:**
  - **Tên không có gạch ngang** (`<view>`, `<content>`, `<box>`, `<section>`, `<grid>`, `<card>`, `<text>`, `<button>`, `<input>`, `<list>`, `<item>`, `<dialog>`, `<dropdown>`, `<tabs>`…) thuộc từ vựng chuẩn của Kitwork. Thẻ không gạch ngang ngoài từ vựng là lỗi.
  - **Tên có gạch ngang** (`<product-card />`, `<shop-cart>`) là mảnh layout hoặc component của ứng dụng/site (xem [`layout.md`](layout.md) và [`component.md`](component.md)).
- **Không viết class CSS trong `.ui`:** Mọi thuộc tính trình bày được viết bằng các thuộc tính có kiểu trên thẻ (`padding="4"`, `gap="3"`, `background="paper"`, `color="ink"`, `radius="medium"`), lấy giá trị từ bảng token thiết kế (`design.work` — xem [`design.md`](design.md)). Bảng màu mặc định: `canvas`, `paper`, `panel`, `ink`, `line`, `brand`, `success`, `warning`, `danger`, `info`; tên khác phải khai trong `design.colors`, không thì là lỗi. Nhạt hơn `ink-30`, đậm hơn `brand+15`, độ hiện `ink/80`.
- **Thuộc tính lạ là lỗi:** thuộc tính trên thẻ của Kitwork không phải chữ nào của `.ui` là lỗi ở đúng chỗ, kèm chữ gần nhất.
- **Không có `<script>` hay `<style>` trong `.ui`:** Logic nằm hoàn toàn trong `.work`.

---

## 2. Nhóm thẻ chuẩn của Kitwork

### 2.1. Khung và Vùng ngữ nghĩa (Regions)

- `<view>`: Gốc của file `view.ui` (ánh xạ sang `<body>` trên web).
- `<content>`: Nội dung chính của route trong `content.ui` (hoặc thẻ rỗng `<content />` đặt trong `view.ui`).
- Các vùng ngữ nghĩa khác: `<header>`, `<navbar>`, `<sidebar>`, `<tabbar>`, `<overlay>`, `<footer>` (xem [`layout.md`](layout.md)).

### 2.2. Bố cục (Layout Primitives)

- `<section>`: Một dải nội dung của trang.
- `<box>`: Khung chứa trung lập (con xếp theo cột mặc định; đổi hướng bằng `flex="row"`).
- `<grid>`: Bố cục lưới (`columns="1" medium:columns="3" gap="4"`).
- `<card>`: Bề mặt nền `paper`, bo góc, không viền.

### 2.3. Chữ, Hình ảnh và Nội dung phong phú

- `<text>`: Mọi văn bản hiển thị (`heading="1".."6"`, `size="mini"|"small"|"medium"|"large"|"huge"`, `weight="medium"|"bold"`, `color="ink-30"`).
- `<richtext value="{{ post.bodyHtml }}" />`: Hiển thị nội dung HTML phong phú do người dùng soạn thảo. **Engine tự động làm sạch (sanitize) ở server theo danh sách thẻ/thuộc tính an toàn** trước khi xuất ra, chặn hoàn toàn XSS mà không cần gọi hàm làm sạch trong `.work`.
- `<image value="{{ product.imageUrl }}" alt="{{ product.name }}" />`: thiếu `alt` là cảnh báo (`alt=""` khi chỉ trang trí).
- `<icon value="shopping-cart" />`: icon Tabler (tên sai là lỗi kèm tên gần nhất). Không có `<logo>`.
- `<code>`, `<quote>`, `<time>`.

### 2.4. Danh sách

- `<list>` với `<item>` và `<empty>`. `<empty>` luôn vẽ (`<li role="status">`); ẩn khi có mục thì đặt `if` trên nó:
  ```html
  <list gap="2">
    <item for="order of orders; key: order.id">
      <text>#{{ order.id }} — {{ order.customer }}</text>
    </item>
    <empty if="orders.length == 0">Chưa có đơn hàng nào.</empty>
  </list>
  ```

### 2.5. Điều khiển và Tương tác (Controls)

- `<button click="save()" disabled="{{ save.pending }}">Lưu</button>`
- `<link value="/san-pham/{{ item.id }}">{{ item.name }}</link>`
- `<form submit="createOrder()">...</form>`
- `<field>`, `<label>`, `<input>`, `<textarea>`, `<select>`, `<option>`. Ô chọn: `<input type="checkbox" />`, `<input type="radio" />`; công tắc: `<input type="checkbox" role="switch" />`.

---

## 3. Biểu thức, Điều kiện và Vòng lặp

### 3.1. Nội suy `{{ ... }}` và Ràng buộc hai chiều

- Mọi giá trị trong `{{ ... }}` đều được tự động thoát ký tự HTML (escape) ở cả server lẫn client.
- Trên ô nhập (`<input value="{{ query }}" />`, `<input type="checkbox" checked="{{ agreed }}" />`), `value`/`checked` là một tên trơn của `var` thì **hai chiều**: người dùng gõ vào ô thì biến `var` đổi ngay; biểu thức khác tên trơn thì một chiều.

### 3.2. Điều kiện (`if` và `show`)

- **`if`:** server bỏ phần tử khi sai; điều kiện ở trình duyệt thì vẽ ẩn, kernel ẩn/hiện. Không có `else`/`else-if`: viết `if` ngược lại.
  ```html
  <box if="user">
    <text>Xin chào {{ user.name }}</text>
  </box>
  <box if="!user">
    <link value="/login">Đăng nhập</link>
  </box>
  ```
- **`show="biểu_thức"`:** Phần tử luôn có mặt trong DOM, ẩn/hiện tức thì ở client (thích hợp cho thông báo lỗi `show="save.error"` hoặc tab đổi nhanh).

### 3.3. Lặp danh sách (`for` và `key`)

```html
<item for="product, index of products; key: product.id">
  <text>{{ index + 1 }}. {{ product.name }}</text>
</item>
```

- **Khoá viết trong `for`, sau `;`** (`for="x of list; key: x.id"`). Khoá chỉ dùng cho danh sách chạy ở trình duyệt (giữ đúng dòng khi thêm, xoá, đổi thứ tự); mẫu thông báo của `toast` bắt buộc có. `key=` viết riêng bị bỏ qua.

---

## 4. Sự kiện và Tiền tố trạng thái / màn hình

### 4.1. Sự kiện

Viết trực tiếp tên sự kiện trên thẻ điều khiển:
- `click="remove(item.id)"`
- `submit="save()"`
- `change="onSelect()"`, `input="onType()"`: `$event` là sự kiện DOM gốc (`$event.target.value`); `$event.value`/`.label` chỉ có ở `change` của autocomplete. Ô nhập dùng `value="{{ var }}"` hai chiều.
- `keydown:enter="send()"` / `keydown:escape="close()"`
- `intersect-enter`, `intersect-leave`: chạy khi phần tử vào/ra màn hình.

### 4.2. Tiền tố trên thuộc tính trình bày (Modifiers)

- **Theo kích thước màn hình (Responsive):** `small:`, `medium:`, `large:`, `wide:`, `huge:` (ví dụ: `columns="1" medium:columns="2" large:columns="4"`).
- **Theo trạng thái:** `opened:`, `active:`, `selected:`, `scrolled:`, `hover:`, `focus:`, `pressed:`, `disabled:` (ví dụ: `background="brand" hover:background="brand+15"`). `open:` là lỗi (nay là `opened:`).
- **Theo đích:** `website:`, `desktop:`, `mobile:`, `ios:`, `android:`, `windows:`, `macos:`, `linux:`.
- **Ghép:** thứ tự đích → độ rộng → trạng thái, nối bằng `+` (`medium+hover:background`); cùng trục nối bằng `,`.
- **Theo chế độ sáng/tối:** Các màu ngữ nghĩa trong `design.work` đã tự động đổi theo `light`/`dark`; `dark:` không phải tiền tố (lỗi).

---

## 5. Phần tử và component của Kitwork hôm nay

- `appearance`: vẽ thành `<button>`; `mode` (`"system"` khi chưa chọn), `dark`, `toggle()`, `set(mode)`, `system()`.
- `dropdown`: `opened`; `open()`, `close()`, `toggle()`; `<popover>` bên trong hiện theo `opened`.
- `toast`: `<toast as="$toast">` một lần; `show()`, `info()`, `success()`, `warning()`, `danger()`, `loading()`, `update()`, `dismiss()`, `clear()`; mẫu thông báo là con trực tiếp `for="notice of notices; key: notice.id"`.
- `network`: `connected`, `opened`, `close()`; sự kiện `offline`, `online` chỉ trên chính thẻ.
- `tabs`: `selected`, `persist`, `select('tên')`; tab là `<button click="select('…')">`, panel là phần tử có `show="$x.selected == '…'"`.
- `carousel`: `current`, `count`, `playing`; `go(i)`, `next()`, `previous()`, `play()`, `pause()`, `toggle()`; `<list>` bên trong là dải trang.
- `countdown`: `duration` (1s–24h), `remaining`, `hours`, `minutes`, `seconds`, `playing`; `play()`, `pause()`, `toggle()`, `stop()`; sự kiện `finish`.
- `editor`: `empty`, `words`, `characters`, `unsaved`, `opened`, `address`, `format`, `dropped`; `toggle()`, `align()`, `rule()`, `link()`, `unlink()`, `undo()`, `redo()`, `close()`.
- `richtext`: server vẽ cả, `value` là một `{{ }}` đọc dữ liệu trang, làm sạch ở server.
- `reader` (`text`, `say()`), `progressbar` (`active`, `value`): site có Drive mà khung không viết thì engine tự thêm.
- `terminal`: `typing`, `copied`, `copy()`; mỗi `<code>` bên trong là một lệnh (ít nhất một).
- `breadcrumb`: `<nav>` chứa danh sách các `<item>`; link trong item là một bước, `<text>` không chứa phần tử nào là trang đang xem; kèm dữ liệu BreadcrumbList.
- `dialog`: `<dialog as="$x" keydown:escape="close()">` vẽ một lần; `opened`, `value` (thứ `open(x)` đưa, `null` khi đóng), `open(x)`, `close()`, `confirm()`; `<dialog>` của trình duyệt mở bằng `showModal()`. Trong `for`, thiếu `as`, `show` trên dialog là lỗi.
- `accordion`, `accordions`: `<details>`; con đầu mang `click="toggle()"` là `<summary>`; `opened="true"` hay `{{ }}`; nhóm đặt `name` chung (mở mục này thì mục kia đóng), `multiple="true"` bỏ name; nhóm có `as` thì `close()` đóng mọi mục. `open` là lỗi.
- `avatar`, `avatars`: `size` `mini`, `small`, `medium` (mặc định), `large`, `huge` (24–64px); ảnh đè chữ cái đầu của `alt`; nhóm vẽ `max` cái và ô "+N".
- `autocomplete`: ô gõ là combobox; `name` nằm trên ô ẩn sau ô gõ, form gửi `value` của mục đã chọn; `value="{{ var }}"` hai chiều; option là danh sách sống của trang thì tìm qua server; chọn xong phát `change` với `$event.value`, `$event.label`.
- Trạng thái mở là `opened`, tiền tố `opened:`; `open:`, hay `open` đọc như giá trị trong component có `opened`, là lỗi.
- Server dịch mọi biểu thức `.ui` chạy ở trình duyệt (bind, class, style, if, for, sự kiện) thành JavaScript trong `/_kitwork/page.<hash>.js`, nạp sau kernel; kernel không tự đọc biểu thức.

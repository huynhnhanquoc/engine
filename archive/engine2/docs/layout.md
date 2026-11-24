# Layout

Đặc tả khung trang (`view.ui`), các vùng ngữ nghĩa (Semantic Regions) và mảnh giao diện dùng lại (Layout Fragments) trong `.ui`.

## 1. Cấu trúc một trang: `view.ui` và `content.ui`

Một trang hoàn chỉnh được lắp ghép từ khung `view.ui` gần nhất (tìm từ thư mục của route ngược lên gốc site) và nội dung `content.ui` của chính route đó:

```html
<!-- view.ui ở gốc site -->
<view background="canvas" color="ink">
  <header />
  <box flex="row">
    <sidebar />
    <content />
  </box>
  <footer />
  <overlay />
</view>
```

```html
<!-- san-pham/content.ui -->
<content padding="6" gap="4">
  <text heading="1">Sản phẩm</text>
  <product-card for="item of products; key: item.id" />
</content>
```

### Luật lắp ghép khung và vùng

- **`view.ui`** có thẻ gốc `<view>`. Thư mục con có thể đặt `view.ui` riêng (ví dụ `quan-ly/view.ui`) để thay khung cho cả nhánh đó. Không có `view.ui` nào thì trang là `content.ui` một mình.
- **`<content />` rỗng trong `view.ui`** là vị trí đặt `content.ui` của route đang mở. File `content.ui` luôn có thẻ gốc là `<content>`.
- **Các thẻ vùng rỗng (`<header />`, `<sidebar />`, `<footer />`, `<overlay />`…):** Engine tự động tìm file `<tên_vùng>.ui` từ thư mục của route ngược lên gốc site. Nhờ đó, một nhánh con chỉ cần đặt `sidebar.ui` riêng là thay được thanh bên cho nhánh đó mà vẫn giữ nguyên `view.ui` của cả site. Thẻ vùng ở chỗ gọi chỉ nhận `source`, `if`, `show`; `source` là tên file tìm như vùng (`<sidebar source="sidebar-admin" />`).
- **Điều hướng giữ khung (Drive):** Drive bật mặc định (`router.reload(true)` tắt). Cùng `view.ui`: chỉ thay `<content>`, các vùng giữ nguyên trạng thái (kể cả khi nhánh có `sidebar.ui` khác). Khác `view.ui`: tải lại cả trang.

---

## 2. Từ vựng các vùng ngữ nghĩa (Semantic Regions)

Tên vùng mô tả **vai trò ngữ nghĩa**, không khoá cứng vị trí hình học:

| Thẻ vùng | Vai trò ngữ nghĩa |
|---|---|
| `<header>` | Vùng đầu cấp ứng dụng/trang (nhận diện, điều hướng chính, thao tác toàn cục). |
| `<navbar>` | Điều hướng chính giữa các khu vực của site. |
| `<sidebar>` | Bề mặt hỗ trợ chính (cây thư mục, danh mục, dự án). |
| `<content>` | Bề mặt làm việc hoặc đọc chính của route hiện tại (ánh xạ sang `<main>` ở cấp trang). |
| `<tabbar>` | Thanh chuyển giữa các ngữ cảnh ngang cấp (điều hướng đáy trên mobile hoặc tab biên tập). |
| `<overlay>` | Lớp nổi dùng chung toàn site (toast, hộp thoại xác nhận, bảng lệnh), sống xuyên qua các lần chuyển route. |
| `<footer>` | Vùng chân trang cấp ứng dụng/trang. |

Chỉ có bảy vùng trên; thẻ vùng khác là lỗi.

---

## 3. Mảnh giao diện dùng lại (Layout Fragments)

Khi một khối giao diện được lặp lại ở nhiều nơi nhưng **không có trạng thái hay logic riêng** (chỉ đọc dữ liệu tại chỗ gọi), ta tách nó thành một file `.ui` có tên chứa dấu gạch ngang:

```html
<!-- product-card.ui -->
<card padding="4" gap="2">
  <text weight="bold">{{ item.name }}</text>
  <text color="brand">{{ item.price }} đ</text>
</card>
```

```html
<!-- Gọi trong content.ui -->
<product-card for="item of products; key: item.id" />
```

### Phân biệt Mảnh Layout và Component

| Đặc điểm | Mảnh Layout (`product-card.ui`) | Component (`shop-cart`) |
|---|---|---|
| **Tên thẻ** | Có gạch ngang (`<product-card />`) | Có gạch ngang (`<shop-cart />`) hoặc tên chuẩn của Kitwork |
| **Có `component.define` trong `.work`?** | Không | Có (`component.define("shop-cart", ...)`) |
| **Phạm vi dữ liệu** | Đọc trực tiếp các tên ở chỗ gọi | Hàm đọc `context.attributes()` và xuất qua `context.bind()`; giao diện đọc tên của `bind`/`sync` trước tên ở chỗ gọi |
| **Nâng cấp từ Layout lên Component** | Khi thêm `component.define` cùng tên và đặt `<product-card>` làm gốc file `.ui`, mọi chỗ gọi `<product-card />` tự động trở thành instance của component mà không cần sửa chỗ gọi |

### Thứ tự tìm file `.ui` cho thẻ rỗng `<tên-thẻ />`

Tìm từ thư mục của chính file đang viết thẻ:
1. Cùng thư mục với file đang viết thẻ.
2. Các thư mục cha lần lượt lên tới gốc site.
3. Với component (có `component.define`): cạnh file `.work` khai nó (từ 17/11/2026; tới đó là `.shared/components/` của
   ứng dụng). Một mảnh không có logic mà nhiều site dùng chung là giao diện của một component (`docs/component.md` mục 4).
- `source` chỉ có trên thẻ vùng; trên mảnh layout `source` là một đầu vào thường, trên component là lỗi.


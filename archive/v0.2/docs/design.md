# Design

Đặc tả hệ thống thiết kế giao diện của ứng dụng và site qua file `design.work` và thư viện `"kitwork/design"`.

## 1. Tách biệt Cấu hình thiết kế (`design.work`) và Định tuyến (`config.work`)

Toàn bộ cấu hình thị giác (bảng màu, dải chuyển màu, phông chữ, tô sáng cú pháp, chế độ sáng/tối, độ tương phản và chuyển cảnh) được đặt trong file **`design.work`**, tách biệt hoàn toàn khỏi `router.work` và `config.work`:

```js
// design.work (ở gốc app, gốc site, hoặc thư mục nhánh)
import * as design from "kitwork/design";

design.appearance("system");
design.contrast("warning");
design.transition("fade");

design.fonts({
  body: "Outfit",
  code: "Fira Code",
});

design.colors({
  brand: "#2563eb",
  canvas: { light: "#f8fafc", dark: "#090d16" },
  surface: { light: "#ffffff", dark: "#111827" },
  ink: { light: "#0f172a", dark: "#f1f5f9" },
});

design.gradients({
  hero: { type: "linear", direction: "to-bottom-right", stops: [{ color: "brand", at: 0 }, { color: "brand+15", at: 100 }] },
});

design.highlight({ keyword: "brand", string: "success", comment: "ink-30" });
```

### Luật của `design.work`

- **Chỉ `design.work` được import `"kitwork/design"`.**
- **Một dòng import chuẩn:** `import * as design from "kitwork/design";`.
- **Chỉ khai báo ở cấp file, mỗi lời gọi một dòng, không nối chuỗi (`chain`), không `export`.**
- **Kế thừa theo cây thư mục:** `design.work` đặt được ở gốc ứng dụng (`app/design.work`), gốc site (`app/shop.com/design.work`) và thư mục nhánh (`app/shop.com/quan-ly/design.work`). Nhánh con kế thừa cấu hình của cha và ghi đè theo từng token.

---

## 2. Bảy từ vựng của `kitwork/design`

Bộ từ vựng của `kitwork/design` là một tập đóng gồm đúng **7 từ**:

| Lời gọi | Giá trị nhận | Kế thừa theo nhánh | Ý nghĩa |
|---|---|---|---|
| `design.colors({ ... })` | Object các token màu (`"#hex"` hoặc `{ light, dark }`) | Gộp từng token, gần nhất thắng | Bảng màu ngữ nghĩa dùng trong `.ui` (`color="ink"`, `background="surface"`) |
| `design.gradients({ ... })` | `{ tên: { type, direction hoặc position, stops: [{ color, at }] } }` | Gộp từng tên, gần nhất thắng | Dải màu dùng trên nền hoặc chữ trong `.ui` |
| `design.fonts({ ... })` | `{ body?, code? }` | Gộp từng vai trò, gần nhất thắng | Phông chữ cho nội dung và mã nguồn; mặc định Google Sans Flex / Google Sans Code; phông engine chưa mang thì boot ghi một dòng, phông hệ thống vẽ |
| `design.highlight({ ... })` | `{ loại: màu hoặc { light, dark } }` (`keyword: "brand"`, …) | Gộp từng loại, gần nhất thắng | Màu từng loại chữ của khối mã |
| `design.appearance(mode)` | `"system"`, `"light"`, `"dark"` | Gần nhất thắng | Chế độ sáng/tối mặc định trước khi người dùng tự chọn |
| `design.contrast(level)` | `"warning"` (mặc định), `"strict"` | Gần nhất thắng | Màu thiếu tương phản là cảnh báo hay lỗi; engine2 nhận nhưng chưa dùng |
| `design.transition(kind)` | `"none"`, `"fade"`, `"slide"` | Gần nhất thắng | Hiệu ứng chuyển trang khi điều hướng bằng Drive (bật mặc định) |

- Gọi bất kỳ từ nào ngoài 7 từ trên trong `design.work` là lỗi lúc boot kèm gợi ý từ gần nhất.
- Viết các từ này trên `router` (như `router.colors(...)` hay `router.fonts(...)` cũ) sẽ bị `router` từ chối lúc boot và chỉ dẫn chuyển sang `design.work`.

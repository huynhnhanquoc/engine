# Napas

Đặc tả tạo chuỗi thanh toán QR theo chuẩn EMVCo / NAPAS 247 (VietQR) qua thư viện `"kitwork/napas"` (`library/napas`).

## 1. Tổng quan

Thư viện `"kitwork/napas"` dựng chuỗi dữ liệu EMVCo QR (kèm mã kiểm tra CRC-16/CCITT-FALSE `6304`) để hiển thị mã QR chuyển khoản ngân hàng Việt Nam mà không cần gọi API bên thứ ba:

```js
import * as napas from "kitwork/napas";

const qrString = napas
  .bank("vcb", "0123456789")
  .amount(150000)
  .info("Thanh toan don 1024")
  .receiver("CONG TY KITWORK")
  .city("HO CHI MINH")
  .dynamic()
  .payload();
```

---

## 2. Từ vựng chuỗi dựng (`kitwork/napas`)

Mỗi bước gọi trả về một bản sao mới, bản nó được gọi trên giữ nguyên, cho phép tạo một mẫu cố định ở cấp file rồi điền số tiền và nội dung riêng trong từng request mà không bao giờ lẫn dữ liệu giữa hai request:

```js
import * as napas from "kitwork/napas";

// Khai báo mẫu dùng chung ở cấp file
const shopQr = napas
  .bank("vcb", "0123456789")
  .receiver("CUA HANG KITWORK");

// Trong handler của từng request:
const orderQr = shopQr
  .amount(order.total)
  .info("DH " + order.id)
  .dynamic()
  .payload();
```

### Các bước thiết lập (đứng trước `.payload()`)

Mọi bước là phương thức, viết có `()`: `.dynamic` không gọi là một hàm, không phải bước.

| Phương thức | Đối số | Ý nghĩa |
|---|---|---|
| `.bank(codeOrBin, account)` | Tên viết tắt (`"vcb"`, `"tcb"`, `"mbb"`, `"bidv"`, `"vtb"`, `"acb"`, `"vpbank"`, `"tpbank"`, `"sacombank"`…, 32 tên, không phân biệt hoa thường) hoặc BIN 6 chữ số (`"970436"` hay `970436`); rồi số tài khoản (hay số thẻ), bắt buộc, tối đa 55 byte | Ngân hàng thụ hưởng và tài khoản |
| `.toAccount()` | — | Chuyển tới số tài khoản (`QRIBFTTA`, mặc định) |
| `.toCard()` | — | Chuyển tới số thẻ (`QRIBFTTC`): số thẻ là đối số thứ hai của `.bank` |
| `.amount(vnd)` | Số không âm, hữu hạn | Số tiền giao dịch (VNĐ, Tag `54`) |
| `.info(message)` | Chuỗi, tối đa `95` byte | Nội dung chuyển khoản (Tag `62` → Subtag `08`) |
| `.receiver(name)` | Chuỗi, tối đa `99` byte | Tên người nhận (Tag `59`) |
| `.city(name)` | Chuỗi, tối đa `99` byte | Thành phố (Tag `60`, chỉ ghi khi có, không mặc định) |
| `.dynamic()` | — | Mã QR động dùng một lần cho đơn hàng (Point of Initiation Method `12`) |
| `.static()` | — | Mã QR tĩnh dùng nhiều lần (Point of Initiation Method `11`, mặc định) |

---

## 3. Sinh kết quả (`.payload()`) và Kiểm tra hợp lệ

- **`.payload()`** đứng ở cuối chuỗi, kiểm độ dài từng trường rồi trả về chuỗi QR hoàn chỉnh (bắt đầu bằng `000201...` và kết thúc bằng 4 ký tự hex viết hoa của CRC-16).
- **Hỏng thì đóng (kết quả lỗi `INVALID_ARGUMENT` của ngôn ngữ, theo [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe)):**
  - Tại bước gọi: `.bank` với mã không có trong danh mục và không phải BIN 6 chữ số (`bank: BIN must be …`), thiếu số tài khoản (`bank: account number is required`); `.amount` âm, NaN hay vô hạn (`amount: must be a positive number`).
  - Tại `.payload()`: chưa gọi `.bank` (`payload: bank() was not called`); một trường quá 99 byte, kể cả phần đi kèm (tài khoản tới 55 byte, `info` 95, `receiver` và `city` 99), là lỗi nêu bước: `payload: info(…) is too long: a field of a VietQR holds 99 bytes, what it holds besides counted`.
  - Không kiểm bảng chữ: chữ ngoài ASCII được nhận, độ dài tính theo byte.


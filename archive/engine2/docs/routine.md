# Routine

Đặc tả thực thi song song trong bộ nhớ (In-memory Concurrency) qua thư viện `"kitwork/routine"`.

## 1. Khi nào dùng `routine` vs `queue` vs `schedule`

| Thư viện | Lưu trên đĩa? | Sống qua khi khởi động lại? | Mục đích |
|---|---|---|---|
| `"kitwork/routine"` | Không (chỉ trong RAM) | Không | Chạy nhiều việc song song rồi gom kết quả ngay trong một request (`routine.wait`), hoặc tách việc phụ nhẹ không cần lưu vết |
| `"kitwork/queue"` | Có (`kitwork_runs`) | Có | Công việc bền vững cần đảm bảo chạy thành công, tự thử lại khi lỗi, xem lịch sử và phát lại |
| `"kitwork/schedule"` | Có (`kitwork_runs`) | Có | Công việc chạy định kỳ theo thời gian (mỗi phút, hàng ngày, hàng tuần, hoặc một lần vào mốc giờ hẹn trước) |

---

## 2. Chạy song song và Gom kết quả (`routine.go` và `routine.wait`)

Trong `.work`, không có `Promise.all` hay `async/await`. Để gọi nhiều nguồn độc lập cùng lúc và chờ tất cả hoàn tất, dùng `routine.go` kết hợp `routine.wait`:

```js
import * as routine from "kitwork/routine";
import * as http from "kitwork/http";
import { db } from "app/core/shop.work";

router.get((context) => {
  const userId = currentUser(context).id;

  // Khởi chạy 2 tác vụ song song trên 2 VM riêng
  const ordersTask = routine.go(() =>
    db.where((orders) => orders.userId === userId).list(10)
  );
  const shippingTask = routine
    .timeout("3s")
    .retry(1)
    .go(() => http.get("https://shipping.example.com/rates").send());

  // Chờ cả hai hoàn tất theo đúng thứ tự truyền vào
  const [ordersResult, shippingResult] = routine.wait(ordersTask, shippingTask);

  return context.json({
    orders: ordersResult.ok ? ordersResult.data : [],
    shipping: shippingResult.ok ? shippingResult.data.data : null,
  });
});
```

---

## 3. Từ vựng của `kitwork/routine`

### 3.1. Khởi chạy: `[.timeout(d)] [.retry(n)] .go(fn)`

- **`routine.go(fn)`:** Nhận một hàm không tham số `() => ...`, chạy ngay trên một máy ảo (`Runtime`) riêng trong pool của site và trả về một `Task` handle.
- **`routine.timeout("5s").go(fn)`:** Giới hạn thời gian tối đa cho một lần chạy của routine (mặc định `"30s"`).
- **`routine.retry(2).go(fn)`:** Số lần thử lại thêm nếu hàm bên trong trả lỗi qua `return error(...)` hoặc quá hạn `timeout`. Nếu hàm đã thu và xử lý lỗi bằng `safe` rồi trả thành công, routine không tự thử lại vì lỗi đã được xử lý (xem [`language.md` mục 5](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe)).

### 3.2. Chờ kết quả: `routine.wait(...tasks)`

- Nhận một hoặc nhiều `Task` (hoặc một mảng `Task`), chặn hàm hiện tại cho tới khi tất cả các `Task` đó kết thúc, và trả về mảng kết quả tương ứng. Lượt chạy đang chờ kết thúc trước (request bị huỷ, hết hạn) thì `wait` là lỗi `EXECUTION_CANCELLED` bay lên; các `Task` vẫn chạy tiếp.
- Mỗi phần tử kết quả luôn là một object `{ ok, data, error }`:
  - Thành công: `{ ok: true, data: giá_trị_trả_về, error: null }`.
  - Thất bại (sau khi đã hết `retry` hoặc bị `cancel`): `{ ok: false, data: null, error: { code, message, at } }`, cùng hình
    với lỗi của `safe` ([`language.md` mục 5.2](language.md#5-mô-hình-xử-lý-lỗi-error-và-safe)): `at` là chỗ hỏng trong
    hàm của tác vụ, `null` khi không có.

### 3.3. Huỷ tác vụ: `routine.cancel(...tasks)` hoặc `task.cancel()`

- Dừng ngay các `Task` đang chạy; nếu một nơi khác đang `routine.wait(task)` thì kết quả nhận được là `{ ok: false, data: null, error: { code: "CANCELLED", message: "..." } }`.

---

## 4. Cô lập bộ nhớ và Giới hạn tài nguyên

- **Chụp giá trị theo bản sao (Snapshot Isolation):** Các biến từ phạm vi ngoài mà hàm `fn` trong `routine.go(fn)` đọc được đóng băng/sao chép sang máy ảo của routine; routine không bao giờ ghi ngược vào biến cục bộ của hàm gọi và không chia sẻ con trỏ biến đổi với request gốc (tránh hoàn toàn Data Race).
- **Không truyền `context` của HTTP vào `routine.go`:** Routine có thể sống lâu hơn request HTTP (khi không gọi `routine.wait`); mọi thông tin cần từ request (`userId`, dữ liệu body) được đọc ra biến `const` trước khi gọi `routine.go`.
- **Trần số routine đồng thời:** Mỗi site chạy cùng lúc bằng số request của site (`requests` của `app.limits`, mặc định gấp đôi số bộ xử lý, ít nhất 8), thêm tối đa 4096 `Task` chờ chỗ; khi máy chủ tắt, engine báo dừng và chờ các routine đang chạy tối đa 2 giây.


---

## Trạng thái triển khai (16/11/2026)

engine2 làm đúng các mục trên (`library/routine`, `host/routine.go`): `go(fn)` nhận đúng một hàm và trả Task; `wait(...tasks)` hay `wait([tasks])`
trả mảng `{ ok, data, error }` theo thứ tự đưa vào, kể cả khi chỉ một Task; `routine.cancel(...tasks)`, `task.cancel()`;
`.timeout` mặc định 30 giây mỗi lần chạy; Task chạy cùng lúc mỗi site bằng số request của site (1024 khi host không nói),
4096 Task chờ chỗ theo thứ tự `go()`, quá nữa thì Task hỏng `DENIED` ngay (kết quả đọc bằng `wait`). Engine chọn thêm: `wait` nhận một hàm là lỗi nêu cách viết
`go` rồi `wait`; `.retry(n)` từ 0 tới 100; Task hết năng lượng không chạy lại; lỗi của Task không ai `wait` vào log khi lời
gọi đã bắt đầu nó xong. Còn mở: kết quả `{ ok, data, error }` ở đây khác `{ value, error }` của `safe` (`language.md` mục 5).

# Module

Đặc tả file `module.work`, gốc của một ứng dụng, qua thư viện `"kitwork/module"` (`library/module`, `host/module.go`).
Có từ 17/11/2026 (lát 1 của `docs/KITWORK_MODULE_WORK.md` ở repo gốc, nơi giữ vì sao và các lát sau).

## 1. Gốc của ứng dụng

Thư mục chứa `module.work` là gốc của một ứng dụng, như `go.mod` nói một module Go bắt đầu từ đâu. Dưới `app.root`
(mặc định `apps/`), mỗi thư mục `apps/<identity>/` phải có `module.work`; thiếu thì app ấy đóng, lỗi nói phải viết gì,
các app khác vẫn chạy:

```text
shop has no module.work: an app's root is the folder of its module.work, import * as module from "kitwork/module"; …
```

App thiếu `module.work` không có tên miền nào (không suy được), nên không site nào của nó trả 503: tên ấy trả 404.
`app/…` trong import tính từ thư mục chứa `module.work`.
Thư mục có `module.work` cũng chạy được một mình, không cần `apps/` hay `application.work` (mục 7).

## 2. Một dòng import, hai chữ

```js
import * as module from "kitwork/module";

module.timezone("Asia/Ho_Chi_Minh");
module.routes((site) => site.domain);
```

- `module.work` chạy đầu tiên khi app boot, trước mọi file khác của app, một lần.
- Chỉ import `"kitwork/module"`, viết đúng `import * as module from "kitwork/module"`, một lần. Import thư viện khác
  (`"kitwork/router"`…) hay import file (`"./x.work"`, `"app/…"`) là lỗi boot.
- `module.<chữ>(…)` đứng đầu một câu ở cấp file, mỗi lời gọi một dòng (`module.timezone(…).routes(…)` là lỗi); đọc
  `module` như một giá trị (`const m = module`), hay viết trong `if`, vòng lặp, hàm là lỗi boot ở dòng ấy.
- Mỗi chữ viết một lần; lần thứ hai là lỗi nêu chỗ viết lần đầu. Chữ không có là lỗi kèm chữ gần nhất
  (`module has no word timezon: did you mean module.timezone?`). `module.work` không có dòng nào vẫn là gốc của app.
- Không file nào khác import `"kitwork/module"`.

| Chữ | Ý nghĩa |
|---|---|
| `module.timezone("Asia/Ho_Chi_Minh")` | Múi giờ của cả app, tên IANA: `Date` của mọi site, mốc giờ của cache trên route, lịch không viết `.timezone(…)`, `expire` theo mốc lịch của `kitwork/storage`. Không khai thì múi giờ của host, `app.timezone(…)` trong `application.work` (17/11/2026, lát 3), không có nữa thì UTC: gọi lên theo tầng, tầng gần nhất thắng. `"Local"` (giờ của máy chủ) là lỗi |
| `module.routes((site) => …)` | Thư mục của từng tên miền host giao cho app (mục 3). Một hàm một tham số |

`router.timezone(…)` (trong `config.work` của gốc app, tới 16/11/2026) là chữ cũ: lỗi nêu `module.timezone(…)`.

## 3. Site: tên miền và thư mục

Tên miền của app là những tên host giao cho nó:

- mỗi tên bảng `domain` giao cho `<identity>` (dòng chưa xoá, `confirmed`, không chuyển đi nơi khác);
- khi `module.work` viết `module.routes`: thêm mỗi thư mục ở gốc app mang một tên cục bộ (`shop.test`,
  `shop.localhost`, `localhost`, địa chỉ mạng riêng), tên không ai sở hữu, không cần dòng trong bảng.

Tên không phải hostname (khoảng trắng, ngoặc, chữ ngoài ASCII, ký tự xuống dòng Unicode…) không bao giờ là tên của app:
nó sẽ đứng đầu các dòng log của site.

**Có `module.routes`:** mỗi tên một site. Host gọi hàm một lần cho mỗi tên lúc boot (và mỗi lần app boot lại), với
`{ domain }`, trên một VM riêng trong trần năng lượng và bộ nhớ của một lượt chạy; hàm trả thư mục của site, tính từ gốc
app, có thể sâu hơn một bậc (`"sites/vn"`). Nhiều tên có thể chung một thư mục: mỗi tên vẫn là một site riêng, boot
riêng.

```js
module.routes((site) => site.domain);                                         // mỗi tên một thư mục mang tên ấy
module.routes((site) => site.domain.endsWith(".vn") ? "sites/vn" : "global");  // nhiều tên chung một thư mục
```

Thư mục hàm trả phải có thật trong app, đi thẳng xuống từ gốc (không `.`, `..`, `\`, `:`, ký tự điều khiển), không
bước nào là thư mục chấm hay gạch dưới (`.shared`, `_nhap`), không phải link, không rỗng (gốc là site của app không
viết `module.routes`). Hàm lỗi, trả thứ không phải chuỗi hay thư mục không hợp lệ là lỗi của riêng tên ấy: site của tên
đó trả 503, log một dòng `[<identity>] the site <tên> answers 503: module.routes(…) at module.work:2:7, for <tên> …`,
`engine2.Check` báo lỗi; các tên khác của app vẫn chạy.

`design.work` và `config.work` ở gốc app là cha của mọi site (`config.work` ở cấp app: `language`, `favicon`).
Thư mục ở gốc app mang tên công khai mà bảng không giao cho app thì boot in một dòng
`[<identity>] the site <tên> is not opened: <lý do>`.

**Không có `module.routes`:** app là một site, gốc của nó là gốc app. Trang tính từ gốc: `router.work` ở gốc là `/`,
`san-pham/router.work` là `/san-pham`, một thư mục tên `shop.test` là trang `/shop.test`. `design.work` và
`config.work` ở gốc là của gốc site (`router.reload(…)`, `router.meta(…)` viết được ở đó). Site phục vụ mọi tên bảng
`domain` giao cho app; không tên nào thì app vẫn boot, không phục vụ gì, và log nói vậy. File `.work`, `.ui` và thư mục
chấm không bao giờ được phục vụ tĩnh.

## 4. Code tự khai

Từ 17/11/2026 (lát 2 của `docs/KITWORK_MODULE_WORK.md` ở repo gốc) bảng, job, lịch và luật chung của hàng đợi, của lịch, và (lát 7, cùng ngày) component tự khai ở cấp file của **bất kỳ file `.work` thường nào** của app (không phải `module.work`, `router.work`, `config.work`, `design.work`; `components.work` thôi có vai riêng từ lát 7): boot đọc mọi file như vậy, ở mọi thư mục, trừ thư mục gạch dưới (bản nháp) và thư mục chấm (`.database/`…); `.shared/` và `.schedule/` ở gốc app, chỗ của code tới 16/11/2026, là lỗi boot nêu file đầu tiên và chỗ mới (`.shared/components/` cũng vậy từ lát 7; giao diện dùng lại của component đặt cạnh file khai nó, [`component.md`](component.md) mục 2, 4); `config.work` viết luật chung của hàng đợi, của lịch cũng là lỗi boot, vì file có vai không được đọc cho việc nền. Ví dụ dưới gom code vào `core/`, tên do app chọn; engine không gán nghĩa cho tên thư mục nào. Một file import file khác thì chạy lại khai báo của file ấy: bảng tìm lại theo chỗ viết, luật chung là cùng lời gọi,
job và lịch thuộc file viết ra chúng. Hai lượt: lượt đầu khai bảng (mọi file, `declaring`), lượt sau nạp job, lịch, luật
chung (mọi file, câu lệnh database được dịch như mọi file; file viết luật chung chạy trước). Chi tiết: [`schema.md`](schema.md),
[`queue.md`](queue.md), [`schedule.md`](schedule.md).

## 5. Bản đồ của `kitwork check`

`kitwork check` (`engine2.CheckMap`, `host.Map`) boot như `Check`, không mở cổng, rồi in bản đồ của mọi app nó đọc, theo
identity: múi giờ của app và nơi nó đến (`module.work:…`, `application.work:… (app.timezone, của host)`, hay `(không tầng nào
khai)` cho UTC), `routes` của `module.work`, mỗi site (tên miền và thư mục nó phục vụ, hay `503` kèm lý do), mỗi trang
với cửa của nó (`GET`, `POST`…, `VIEW` cho `router.view`; thư mục chỉ có `content.ui` là `GET …/content.ui`), mỗi bảng
(`connection()`…), job, lịch (cách chạy, quy tắc, múi giờ và nơi nó đến: `(.timezone của lịch)`, `(giờ của on(…))`, `(của app: …)`), luật chung và component, mỗi dòng kèm chỗ khai `file:dòng:cột` tính từ gốc
app. Bản đồ đọc từ những gì boot dựng, không do người viết giữ cho khớp. App bị đóng in các tên của nó `503` và lý do.

```text
app      03dteiwhm2ny4ur7svb9fjc6p8z1a5ogqlxk
múi giờ  Asia/Ho_Chi_Minh                      module.work:6:7
routes   module.routes                         module.work:7:7
site     demo.kitwork.io                       demo.kitwork.io/
trang    demo.kitwork.io /cart                 VIEW demo.kitwork.io/cart/router.work:5:7
bảng     orders                                connection() core/shop.work:19:31
job      camOn                                 core/camOn.work:10:27
lịch     nhip                                  recurring every("30s") Asia/Ho_Chi_Minh core/nhip.work:9:29
```

`kitwork migrate` từ chối app còn `.shared/`, `.schedule/` như boot, thay vì lập kế hoạch thiếu bảng.

## 6. Trần của tenant: bảng `tenant`

Trần riêng của một tenant là dòng của nó trong bảng `tenant` của database `system` của host, cùng file với bảng `domain`
(`app.website({ domains })`, mặc định `.database/system/kitwork.db`), từ 17/11/2026 (lát 4); tới đó là
`app.limits("shop.test", { … })` của từng site, nay là lỗi chỉ sang bảng. Host ghi bảng; tenant không khai trần của mình.

```sql
CREATE TABLE tenant (identity TEXT PRIMARY KEY, status TEXT DEFAULT 'active', memory TEXT, energy INTEGER, requests INTEGER);
INSERT INTO tenant (identity, status, memory, energy, requests) VALUES ('02wkjcx…', 'active', '256mb', 20000000, 64);
```

- Một dòng cho app có identity ấy: `memory` (một lượt chạy tạo được, `"1mb"`–`"16gb"`), `energy` (một lượt chạy tiêu được,
  `100000`–`1000000000000`), `requests` (số request mỗi site của app chạy cùng lúc, `1`–`100000`). Áp cho mọi site của app,
  cả job và lịch của nó. Cột để trống giữ mặc định: `app.limits({ … })` của manifest, không có thì của engine.
- Chỉ dòng `status` là `active` (hay trống) được áp. Dòng trạng thái khác, hay viết sai (`memory` không phải kích cỡ…), boot
  in một dòng và không áp gì: app chạy theo mặc định. Dòng của identity không app nào có cũng được nói. Không có file hay
  không có bảng: mọi tenant theo mặc định.
- Bảng đọc lúc boot và đọc lại mỗi khi file (hay `-wal` của nó) đổi, cùng nhịp với bảng `domain`: app nào trần đổi thì boot
  lại (`reloaded after the table tenant changed`). Bảng đọc lại không được thì giữ bản đọc trước.
- Bản đồ của `kitwork check` in trần của mỗi app và nơi nó đến: `bảng tenant`, `app.limits, mặc định của host`, hay
  `mặc định của engine`.

Lát 5 (bảng `grant` thay `apps:` của `app.databases`) bỏ 17/11: không có ca thật; database của host vẫn cấp bằng
`apps:` ([`application.md`](application.md)).

## 7. Chạy một mình

Từ 17/11/2026 (lát 6 của `KITWORK_MODULE_WORK.md`): thư mục có `module.work` chạy được một mình, `cd shop && kitwork`
(hay `kitwork check`, `kitwork migrate`), không cần `apps/` hay host. Lệnh đọc `application.work` của thư mục; có
`module.work` cạnh nó thì **thư mục ấy là app**, app duy nhất (`host/alone.go`).

**Không có `application.work`:** mặc định của engine, log nói một dòng:

```text
[app] no application.work in D:\shop: the app of its module.work runs alone, on the engine's defaults: port 8080 of this machine alone, no AutoSSL; an application.work beside module.work says otherwise
[app] serving 1 sites on 127.0.0.1:8080
[app] open http://localhost:8080/ (localhost)
```

- Website ở cổng 8080, chỉ máy này (`127.0.0.1`), không AutoSSL; trần, năng lượng, số request của engine; múi giờ của
  `module.work`, không có thì UTC; không hot reload; không database của host, không lệnh, không cổng database
  (`kitwork database` nói phải viết `app.database(…)` trong `application.work` cạnh `module.work`).
- `connection("shopping")`, một database của host, là lỗi của app nói cách khai (câu hỏi 8 của bản nháp):
  `connection("shopping") là database của host: chạy một mình thì không có. Thêm application.work cạnh module.work:
  app.databases({ shopping: { sqlite: "./shopping.db" } })`. Code của app không đổi.

**Có `application.work` cạnh `module.work`:** đó là cấu hình thật của app, đọc như manifest của host (cổng, AutoSSL,
`app.limits`, `app.databases`, `app.logger`, `app.commands`…), trừ hai chữ của host có nhiều app: `app.root(…)` và
`app.website({ domains })` là lỗi boot (thư mục của manifest là app; không bảng nào giao tên). `application.work` không
bao giờ là code của app: lượt tự khai bỏ qua nó.

**Cả hai cách:**

- Không bảng nào của host được đọc (`domain`, `tenant`): app duy nhất có mọi tên. Có `module.routes`: mỗi thư mục ở gốc
  mang tên một hostname, công khai hay cục bộ (`shop.example/`, `blog.test/`; thư mục một chữ như `core/` là code), là
  một site. Không có: app là một site ở gốc, tên của nó là `hostname` của `app.website`, hay `localhost` khi không
  manifest nào viết.
- Trên máy dev (`allowLocal`), `<tên>.localhost` tới site `<tên>`; `localhost` và `127.0.0.1` tới site của
  `app.website({ hostname })`, không có thì tới site duy nhất của app (nhiều site thì không site nào, log in địa chỉ từng
  site).
- Mọi database của `app.databases` mở cho app, không cần `apps:`.
- File `.env`, `.work`, `.ui` ở gốc không bao giờ được phục vụ tĩnh.

**Đưa lên host:** chép nguyên thư mục vào `apps/<identity>/` là chạy, không sửa dòng nào. `application.work` của nó (nếu
có) không được đọc ở đó; boot in một dòng `[<identity>] application.work at the app's root is its own when it runs alone;
under this host it is not read: application.work says what the app has`.

Chưa có: tầm với rộng hơn khi chạy một mình (bảng mục 5 của bản nháp: `kitwork/file` ra ngoài thư mục app), vẫn giữ như
tenant; nguồn của từng giá trị in ở log lúc boot (hôm nay chỉ trên bản đồ).

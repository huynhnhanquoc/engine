# blueprint

Thứ `application.work` trả về, viết thành struct Go: mỗi cửa một package, context của một hàm nằm cạnh cửa gọi nó.

**Thụ động (23/11/2026, Application, Website, Database, Schedule và Command; desktop, mobile chưa):** chương trình chạy trên VM chỉ điền giá trị vào
các trường; blueprint giữ đúng các `value.Value` ấy, không kiểm, không hiểu; engine nạp bản thiết kế, kiểm mọi trường
một lần trước khi mở gì (gom mọi lỗi theo đường dẫn trường: `application.website.listen[1]: …`), đóng băng cả cây
(`value.FreezeShared`), rồi đọc động từ (`Running()`, `Checking()`): `app.run()` thì mở các cửa có mặt; `app.check()`
thì chỉ nạp và kiểm; không có cái nào là lỗi boot, và cả hai thì không viết ra được (cái viết sau bị từ chối).

**Cách viết (owner 24/11/2026, mục "Cách viết `application.work`" dưới):** `const app = application(…)`, mỗi cửa một
câu `app.<cửa>(cấu hình).<hàm>….<chữ cuối>(giá trị chính)`, `return app.run()`. Chỉ `app` dựng được cửa: thư viện
`"application"` đưa ra đúng hai tên, `application` và `environment`.

**Chạy thật (23/11/2026):** `host.Program{File, Source, Folder}.Boot()` (hay `host.Boot(file, source)`) biên dịch và
chạy `application.work` bằng VM của engine3 (`core/syntax` → `core/compiler` → `core/runtime`) và trả bản thiết kế,
cùng các ghi chú của boot. Chữ của struct gọi qua reflect theo đúng luật chữ JS dưới đây (chỉ lúc boot). Một hàm mũi tên
là `runtime.Closure`, một `value.Sharer`: `FreezeShared` đi tới biến nó bắt, và gán vào biến ấy sau khi nạp là lỗi.
Lỗi biên dịch và lỗi lúc chạy có `file:dòng:cột`. `go run ./cmd/demo` chạy mười bốn `application.work` như thế.

**Mốc nghiệm thu (23/11/2026):** `application.work` của `docs/host/blueprint` boot trên engine3
(`test/host/acceptance_test.go`; bản chép ở `test/host/testdata/blueprint`), chỉ đổi những gì đã chốt:
`from "application"`, `environment("./.env")` thay `file.env`, `schedule` thay `schedules`; 24/11 bản chép viết lại
theo cách viết mới (`app.<cửa>(…)`, chữ cuối, `return app.run()`), nội dung giữ nguyên.
`host/system.work` và `host/maintenance.work` ở đó là bản giả lập (engine3 chưa có KitDB): cùng các export, dữ liệu
trong RAM. Sau boot, test gọi các hàm của bản thiết kế trên VM như engine sẽ gọi: các thư mục có lịch, memory của một
job, ba lệnh của `kitcli`, routes.

Ngôn ngữ runtime nhận thêm cho mốc này (mỗi luật có một đối chứng: tắt code thì test đỏ):
- `for (let i = 0; i < n; i++)`: mỗi vòng một bản của biến đếm khi closure bắt nó, như JavaScript;
  `for (const x of list)`: mảng, hay chuỗi theo ký tự; thứ khác là lỗi TYPE. `break`, `continue`, `++`, `--` (tiền tố,
  hậu tố; hậu tố cho giá trị cũ). Không `while`, `do`, `for (;;)`.
- giải cấu trúc object khi khai báo: `const { request } = context`, `const { a: { b }, c = 1 } = x` (lồng, đổi tên,
  mặc định). Chưa: mảng, `...rest`, khoá tính, giải cấu trúc tham số (lỗi biên dịch nói viết `(context) => context.x`).
- spread trong mảng và object: `[...a, ...tenants.map(f)]` (mảng hay chuỗi), `{ ...a, b }` (như `Object.assign`).
  Chưa: `f(...list)`.
- vẫn là lỗi biên dịch "later slice": `throw`, `switch`, `?.`, `new`, regex, bitwise, `in`, `instanceof`, `&&=`.

**File của chương trình (`host/files.go`):** `application.work` import file khác bằng đường dẫn tương đối, các file ấy
`export const`:
```js
import { system } from "./host/system.work";            // tính từ thư mục của file import
import { backup, migrate } from "./host/maintenance.work";
```
- đường dẫn trong thư mục của `application.work` (`../` ra ngoài là lỗi), viết cả `.work` (lỗi nói tên đúng);
- mỗi file chạy một lần mỗi lần boot, trên cùng VM của boot, dù nhiều file import nó: các file thấy cùng một biến;
- vòng import là lỗi nêu cả vòng (`application.work → a.work → b.work → a.work`);
- file được import chỉ import file khác: thư viện (`"application"`) là của `application.work`; nó không `return`;
- lỗi trong file (biên dịch, lúc chạy, tên không export) có `file:dòng:cột` của file ấy.

**Trần byte (23/11/2026; `core/runtime/memory.go`, `core/javascript/meter.go`, `core/value/footprint.go`):** mỗi lượt
chạy có trần byte, `runtime.Options{Memory}`, mặc định 64 MiB như một request của engine2 (boot dùng mặc định). Lấy bài
học của engine2, viết gọn:
- đếm cái lượt chạy làm ra: lệnh (chữ của `+` và template, mảng, object, `[...a]`, `{ ...a }`, closure, ô của biến bị
  bắt, khoá hay phần tử một phép ghi thêm vào) và hàm có sẵn (kết quả như nó tự giữ, trừ khi là một phần của cái nó
  được đưa; cái nó thêm vào this hay đối số đầu, như `push`, `Object.assign`);
- hàm làm ra nhiều từ ít hỏi chỗ trước khi cấp phát: `repeat`, `padStart`/`padEnd`, `concat`, `split`, `join`,
  `Array(n)`, `Array.from`, `[...s]`, `flat`, `JSON.parse`, `JSON.stringify`, `Object.entries`, `encodeURI…`,
  `t.format`. `"y".repeat(5e8)` dừng trước khi Go cấp 500 MB;
- quá trần thì đếm lại bằng cái còn với tới: ngăn xếp (closure và các ô của mỗi khung nằm ngay sau slot của nó; this
  và đối số của một hàm có sẵn ở lại trên ngăn xếp tới khi nó trả), và các phần tử `map`, `flatMap`, `Array.from`,
  `groupBy`, reviver của `JSON.parse` đang dựng. Giữ quá nửa trần là `MEMORY_LIMIT`, mã thứ năm của tập đóng. Vậy trần là
  đỉnh: cái lượt chạy giữ (không quá nửa) cộng cái nó làm ra từ lần đếm trước. Một chuỗi dài nhiều chỗ giữ đếm một
  lần; dữ liệu dùng chung từ lúc boot (FreezeShared) là của mọi lượt chạy, không đếm;
- mỗi lần đếm lại, cái làm ra từ lần trước tốn năng lượng, một đơn vị mỗi 32 byte: `s += "x"` trăm nghìn lần dừng ở
  `ENERGY_LIMIT` sau vài chục mili giây, không chép hàng GB. Lượt chạy không bao giờ chạm trần thì không tốn gì thêm;
- `MEMORY_LIMIT` dừng lượt chạy ở lệnh ấy, và mọi lệnh sau đó cũng dừng (một hàm có sẵn nuốt lỗi cũng không cứu được).

Test: `test/runtime/memory_test.go`; 34 đối chứng (tắt từng chỗ đếm, từng chỗ hỏi, từng gốc của lần đếm lại) đều đỏ.
Đo xen kẽ với bản trước bộ đếm (23/11): fib(20) nhanh hơn khoảng 2,5% (đối số của CALL thành slot tại chỗ, không chép
nữa), guard (Logic) và Boot ngang nhau, chương trình làm ra nhiều (Make: object, mảng, chuỗi, map, JSON) chậm hơn
khoảng 5%. So với engine2 cũng trần 64 MiB, cùng máy, xen kẽ: fib 1,33 ms với 2,44 ms; Make 368 µs với 380 µs.

Chưa có: đi vào struct của engine (các cửa) khi đếm lại; nối `memory` của các cửa vào `Options.Memory` (khi request,
job, lệnh chạy thật); trần chung của host.

**Năng lượng cho việc của engine (23/11/2026; `core/javascript/work.go`):** năng lượng đếm lệnh, mà một lệnh gọi một
hàm có sẵn trên dữ liệu lớn làm việc hàng mili giây (`s.endsWith("x")` trên 1 MB tiếng Việt 6 ms, sort trăm nghìn số
50 ms, mỗi cái một đơn vị). Việc ấy giờ tốn năng lượng của lượt chạy, tính từ kích thước, không bao giờ từ đồng hồ
(bài học của engine2, viết gọn):
- một đơn vị là một lệnh thường (3,3 ns trên máy đo); mỗi loại việc một tỉ lệ đo trên cùng máy: chuỗi ASCII 16 byte
  một đơn vị, chuỗi có chữ khác 1 byte (đi từng ký tự), so bằng nhau 64 byte, đổi thành số 2 byte; một phần tử so hay
  dời 1, chép 3, join 8, flat 11; một phép so của sort 10 (tính trước khi sort bắt đầu, nên một sort quá năng lượng
  không chạy); một lời gọi hàm của chương trình từ code của engine (map, sort, replace) 27; một khoá Object.keys 10,
  assign 35; một byte JSON.parse đọc 4, JSON.stringify viết 2; một mảnh split 4, một chỗ khớp replace 15;
- việc dưới 256 byte là giá của lệnh, không tính, không kiểm gì; phần lớn lời gọi không tốn thêm;
- chỗ tính: phương thức của chuỗi và mảng (một bảng theo tên, ở một chỗ: `invoke`), lời gọi lại (`call`), chuỗi đổi
  thành số (`toNumber`), so thứ tự hai chuỗi (`Relation`), `s.length` và `s[i]` theo đường chúng thật đi (`Read`: ASCII
  một lần quét, chữ khác đổi thành đơn vị UTF-16), `===` của hai chuỗi dài, `+` và template của chuỗi dài, các hàm
  Object, JSON, URI, `Array.from`;
- hết năng lượng giữa một hàm có sẵn là `ENERGY_LIMIT` ngay (`Machine.Spend`). Việc tính sau khi xong (trừ sort), nên
  một lời gọi vượt năng lượng còn lại đúng bằng việc của chính nó, và việc ấy bị chặn bởi dữ liệu (trần byte).

Trong lúc đo thấy object lớn chậm theo bình phương (Object.keys, assign, `{ ...o }`, JSON.stringify 2,4–4,7 µs một khoá
với 2000 khoá, vì mỗi khoá là một lần duyệt): `core/value/object.go` giờ có chỉ mục khoá → chỗ từ khoá thứ 9 (như
engine2, và như chính file ấy đã ghi sẵn hướng), dựng khi ghi, không khi đọc (object dùng chung đọc cùng lúc được):
còn 72–165 ns một khoá, `o[k]` không còn duyệt.

Test: `test/runtime/work_test.go`: mỗi phép nặng (60 ca) lặp tới khi hết 3 triệu đơn vị, thời gian so với một vòng
cộng tốn cùng năng lượng: 0,4 tới 2,5 lần (trước lát này: tới 148 lần, `a.replaceAll` một triệu chỗ khớp); và một test
tất định: mỗi phép trên dữ liệu lớn tốn ít nhất 1000 đơn vị, trên chuỗi ngắn vài lệnh. `test/value/index_test.go` cho
chỉ mục. 23 đối chứng (tắt từng chỗ tính, từng phần của chỉ mục) đều đỏ; 34 đối chứng của trần byte vẫn đỏ. Đo xen kẽ
với bản trước lát này: fib, guard (Logic), Boot ngang nhau; Make chậm hơn khoảng 1–4%; so với engine2 (trần 64 MiB):
fib 1,4 ms với 2,6 ms, Make 365 µs với 368 µs.

**Con trỏ chữ (23/11/2026; `core/javascript/cursor.go`):** chỉ số của chuỗi là đơn vị UTF-16, string của Go là byte,
nên `s.length`, `s[i]`, `slice`, `substring`, `startsWith`, `endsWith`, `at`, `charAt`, `charCodeAt`, `codePointAt`
từng đi cả chuỗi mỗi lần, có khi ba bốn lần (1 MB: 0,07–6 ms một lời gọi; `for (i < s.length) s[i]` là bình phương).
Bây giờ:
- mỗi phép chỉ đi tới chỗ nó cần: `slice(1)`, `slice(2, 9)` không đếm độ dài, `startsWith(x)` đi bằng độ dài của x;
  đoạn ASCII tám byte một bước, chữ có dấu hai, ba byte đi thẳng; luật đơn vị giữ đúng như `unitReader` (nửa cặp ba
  byte là một đơn vị, byte hỏng là một đơn vị U+FFFD, ký tự bốn byte là hai);
- một lượt chạy nhớ bốn chuỗi dài nhất nó vừa đọc (từ 256 byte; `Machine.Memo`): độ dài, có phải ASCII không, và
  con trỏ (đơn vị nào ở byte nào), nên `s.length` lần thứ hai là O(1), `s[i]` lần lượt đi tiếp, chuỗi ASCII đọc thẳng
  byte. Một lần đếm lại byte quên chúng (chúng là cache, giữ lại thì giữ cả chuỗi chương trình đã bỏ), nên một hàm đổi
  đối số của nó (có thể chạy `valueOf`) trước rồi mới lấy con trỏ;
- năng lượng tính theo phần đã đi; nhớ rồi thì không tốn gì.

Trên chuỗi 1 MB (ASCII hay tiếng Việt) mỗi phép giờ tốn như trên một chuỗi tám ký tự (khoảng 200 ns một vòng, gần hết
là giá của lời gọi). Test: `test/runtime/text_test.go`: so khác biệt 21 000 lời gọi trên chuỗi ngẫu nhiên đủ loại (ASCII,
hai, ba, bốn byte, nửa cặp, byte hỏng, ký tự cụt, viết dài thừa; ngắn và dài; chỉ số tới, lui, âm; xen kẽ bảy chuỗi trên
một lượt chạy, có hai chuỗi cùng số byte) với một bản tham chiếu viết riêng; chuỗi 1 MB tốn như chuỗi ngắn; một lần đếm
lại giữa lời gọi không làm sai kết quả. Đối chứng: tắt từng phần (nhớ, con trỏ đứng ở start, lui về đầu, nửa cặp, cờ
ASCII, giới hạn của bước nhanh, nhận chuỗi theo chỗ, quên khi đếm lại, thứ tự đối số trước con trỏ, năng lượng của bước
đi) đều đỏ. Đo xen kẽ với engine2 (8 lượt, trung vị): fib 1,4 ms với 2,5 ms; Make 368 µs với 365 µs; Text (`slice`,
`startsWith`, `charCodeAt`, `length` trên từ ngắn, bench mới) 211 µs với 238 µs.

Chưa có: `toUpperCase`, `lastIndexOf` đi từng ký tự cả với ASCII; `<` của hai chuỗi đổi cả hai thành đơn vị UTF-16;
`includes`, `indexOf` với một vị trí vẫn đếm độ dài; con trỏ chỉ đi tới (lui là về đầu: một vòng `s[i]` từ cuối về đầu
trên chuỗi không ASCII vẫn bình phương).

**Lời gọi phương thức (23/11/2026):** một lời gọi rẻ (`"ab".slice(1)`, `a.push(x)`, `Math.max(a, b)`) tốn khoảng
150–200 ns: một mảng đối số mới, vài lần băm tên (bảng phương thức của loại, bảng các phương thức không đi qua lỗ, khoá
của `Math` tìm tuần tự trong hơn bốn mươi tên), một phép ép interface để lấy meter. Bây giờ:
- đối số là chính khúc ngăn xếp của chúng (cap cắt ở đó, nên một append không ghi lên ngăn xếp), như CALL một closure:
  không cấp phát; không hàm có sẵn nào giữ mảng đối số quá lúc nó trả (đã rà: `console`, các chữ của cửa qua reflect,
  `add` của các cửa đều chép);
- tên của mỗi lệnh INVOKE tra một lần cho mỗi hàm (`javascript.Name`: phương thức của nó ở mỗi loại), giữ ở
  `compiler.Function.Resolved` (atomic: các request của một site chạy chung một hàm); `Invoke(name)` đi cùng đường;
- `Namespace` có chỉ mục; mảng có lỗ được hỏi trước khi tra bảng các phương thức không đi qua lỗ;
- runtime đưa chính nó làm meter xuống (`InvokeNamed(m, m, …)`), không ép kiểu mỗi lời gọi;
- `Math.max`, `Math.min`, `Math.hypot` đổi đối số vào một mảng trên ngăn xếp;
- lệnh INVOKE nằm ngoài `execute` (`Machine.invoke`): giữ nó trong vòng lệnh làm fib, guard chậm 5–6%, vì một vòng
  lệnh lớn hơn thì mọi lệnh khác chậm theo.

Kèm một lỗi của lát năng lượng: `Math.max(…)`, `o.f()` (một hàm riêng của object) đi qua `call`, nên tốn 27 đơn vị của
một lời gọi lại từ code của engine; giờ đi qua `dispatch`, như một lệnh CALL.

Test: `test/runtime/calls_test.go`: 3000 lời gọi không cấp phát gì mỗi lời gọi; cùng một lệnh INVOKE gặp this của mọi
loại; tám goroutine cùng chạy một hàm dùng chung (`-race`); và `Math.max`, `o.f()` tốn vài lệnh (work_test.go). Đối chứng
(mảng đối số cấp mới, tên chỉ tra một loại, `dispatch` tính giá lời gọi lại, không đưa meter xuống, chỉ mục của Namespace
sai, `Math` cấp mảng số) đều đỏ. Đo xen kẽ với bản trước lát này (8–10 lượt, trung vị): Calls (bench mới: `push`, `slice`,
`Math.max`, `indexOf`, `at`) 1089 → 605 µs, cấp phát 7015 → 1015; Text 234 → 160 µs; Make 495 → 474 µs; fib, guard (Logic),
Boot ngang. Với engine2: Calls 584 µs với 1081 µs; fib 1,39 với 2,54 ms; Text 148 với 231 µs; Make 362 với 362 µs.

Chưa có: một lời gọi rẻ còn khoảng 100 ns mà tính một đơn vị (giá của CALL, INVOKE chờ owner).

**Cấp phát (23/11/2026):** Make (object, mảng, chuỗi, map, filter, JSON) cấp phát 5043 lần mỗi lượt, gần gấp đôi engine2
(2854). Theo profile, từng nguồn:
- `isIndex` (Keys hỏi mọi khoá của mọi object JSON.stringify viết) và `arrayIndex` gọi `strconv.ParseUint`, mỗi khoá
  không phải số (`"id"`, `"name"`) là một lỗi cấp phát hai lần: 1200 lần mỗi lượt. Giờ đếm chữ số, không qua strconv;
- một chuỗi trong `Value.data any` cấp 16 byte: `value.Value` giờ giữ byte đầu của chuỗi trong data (một con trỏ trong
  interface không cấp phát) và độ dài trong number (`core/value/value.go`); `text()` đọc thẳng word dữ liệu của
  interface, không ép kiểu (ép kiểu ở đó làm guard chậm 4%, vì mỗi lần đọc thuộc tính dựng lại tên khoá): 1000 lần;
- mỗi lời gọi lại của map, filter, find, reduce, sort, `Array.from`, groupBy, replace cấp một mảng đối số: giờ một
  mảng cho cả vòng (`callArguments`; bên được gọi chép hay chỉ đọc, không giữ): 400 lần;
- `value.Array(items...)` chép một mảng bên gọi vừa dựng: `value.ArrayOf(items)` nhận luôn nó, cho lệnh MAKE, `[...a]`,
  map, filter, flat, slice (`ItemsBetween` chép đúng khúc), concat, split, Object.keys/values/entries, sort; `Array.of`,
  `Array(a, b)` vẫn chép, vì đối số là một khúc ngăn xếp: 200 lần;
- chữ của các số nguyên 0 tới 1023 dựng sẵn (`"item " + i`): 380 lần;
- lệnh MAKE nằm ngoài `execute` (`Machine.make`), như INVOKE.

Make: 5043 → 1861 lần, 393 → 286 µs (−27%); Text 803 → 213 lần, −15%; Calls 1015 → 15 lần (không lời gọi nào cấp phát),
−11%; Boot 534 → 475 lần; fib, guard ngang (đo xen kẽ 8–10 lượt với bản trước lát này). Với engine2: Make 254 với 365 µs
(1861 với 2854 lần), Text 129 với 234 µs, Calls 570 với 1016 µs, fib 1,41 với 2,53 ms. Chuỗi rẻ đi thì vài giá năng
lượng đo trước đó thành dư (Object.values 0,2×, toSorted 0,3×): đo lại, một khoá Object.keys 10, một phép so của sort
10, một mảnh split 4, và một cặp Object.entries 25 (nó dựng một mảng hai phần tử mỗi khoá).

Test: `TestMakeAllocates` giữ ngân sách cấp phát (số lần cấp phát là tất định với một bản Go); đối chứng (khoá qua
strconv, mảng đối số mỗi lời gọi lại, mảng mới chép lại, chữ của số nhỏ không dựng sẵn) đều vượt nó.

**Mảng khoá dùng chung cho object literal (owner 23/11/2026; `core/value/object.go`: `Shape`):** mỗi object literal
cấp phát ba lần (cấu trúc, mảng khoá, mảng giá trị), mà mọi object của một literal có cùng các khoá. Bây giờ:
- compiler dựng một `value.Shape` cho mỗi literal có khoá viết thẳng không trùng nhau (các khoá theo thứ tự, chỉ mục từ
  khoá thứ 9, thứ tự của `Keys`), đặt nó vào hằng; lệnh `MAKE Shaped` chỉ lấy các giá trị trên ngăn xếp (bớt luôn một
  `PUSH` mỗi khoá) và làm một object mượn khoá của Shape (`ObjectFrom`). Khoá của Shape là chính chuỗi của hằng cùng
  chữ, như tên của một lệnh GET: hai chuỗi cùng chỗ so bằng nhau ngay (khoá lấy từ cây cú pháp làm guard chậm 4%). Một
  khoá viết hai lần (`{ x: 1, x: 3 }`), hay một đoạn có spread, đi đường cũ;
- mượn là chép-khi-ghi: ghi lên một khoá đã có chỉ đổi giá trị; thêm hay xoá một khoá thì object chép khoá (và chỉ mục)
  thành của riêng nó trước (`own`). Shape chỉ đọc, nên các lượt chạy của một site dùng chung nó;
- vì khoá mượn không bao giờ bị sửa tại chỗ, JSON.stringify đọc chúng không chép (`ReadKeys`), kể cả khi một `toJSON`
  thêm khoá vào object đang được viết; khoá của riêng một object thì vẫn chép;
- bộ đếm byte đếm một object mượn khoá bằng các giá trị của nó (khoá là hằng của hàm).

Make: 1861 → 1461 lần cấp phát, 279 → 233 µs (−17%), đo xen kẽ 8 lượt với bản trước lát này; Text ngang. Mỗi lần biên
dịch một literal tốn thêm hai lần cấp phát (Shape, mảng khoá): Boot 475 → 485 lần. Fib, Calls, Logic lệch 1–2,5% (10
lượt); thí nghiệm cùng bản code tắt Shape trong compiler cho Logic chậm hơn (215 với 210 µs), nên phần lệch còn lại là
bố cục code, không phải Shape.

Test: `test/value/shape_test.go` (chép-khi-ghi với 2 tới 12 khoá, có và không chỉ mục; `ReadKeys` giữ nguyên khi object
đổi khoá sau đó; object đóng băng; tám goroutine cùng dùng một Shape, `-race`), các ca của object literal trong
`language_test.go` so với node (khoá viết hai lần, khoá dạng chỉ số, mười khoá, `toJSON` sửa object đang được viết),
ngân sách của Make hạ xuống 1500. Đối chứng (không chép khi thêm khoá, khi xoá khoá, chép khoá mà vẫn dùng chung mảng,
khoá trùng đi đường Shape, `ReadKeys` sai thứ tự, JSON chép khoá, literal không mượn Shape) đều đỏ. Chưa có: `delete` của
ngôn ngữ (chép-khi-ghi của Delete thử ở test Go).

**`{ ...o }` mượn khoá của o (owner 24/11/2026; `value.Assign`, `javascript.Spread`, `compiler.object`):** trước đây
`{ ...o, a }` dựng một object tạm cho mỗi đoạn khoá viết thẳng, rồi gộp mọi đoạn bằng `Object.assign` lên một object
rỗng: mỗi khoá chép một lần, ghi một lần, mảng khoá lớn dần. Bây giờ:
- object có `...` dựng dần theo thứ tự viết, như JavaScript. Đoạn đầu là một object literal, hay `MAKE Copy` (bản chép
  của o, chỗ cho mọi khoá viết thẳng sau nó); mỗi `...o` sau đó chép vào ngay khi gặp (`MAKE Merge`); mỗi đoạn khoá
  viết thẳng ghi thẳng vào object đang dựng (`MAKE Extend`, các khoá là một Shape ở hằng), không object tạm. Vì vậy
  `{ ...o, a: f() }` chép o trước khi f chạy. Trước đây mọi đoạn tính xong mới gộp, nên một f sửa o thì bản chép thấy
  chỗ sửa (node cho `{ x: 1, … }`, engine3 cho `{ x: 5, … }`). Lỗi của một `...` (`{ ...Math }`) chỉ vào chính nó;
- chép vào một object chưa có khoá thì mượn khoá thay vì ghi từng khoá: object mới mượn Shape o đang mượn. Khoá của riêng
  o (JSON.parse, `o.k = v` dựng) thành một Shape cả hai cùng mượn, và o chép khoá lại khi nó thêm hay xoá khoá.
  `Object.assign({}, o)` đi cùng đường. Ghi vào một object đã có khoá vẫn ghi từng khoá, nhưng đọc khoá không chép;
- dữ liệu dùng chung có Shape từ lúc `FreezeShared` (khi chưa request nào đọc), nên bản chép của mọi lượt chạy mượn nó mà
  không ghi gì lên nó;
- khoá của riêng một object lượt chạy làm ra, khi thành Shape, vẫn là của lượt chạy: `Holding` đếm chúng một lần dù bao
  nhiêu object mượn (`Shape.held`). Khoá của hằng hay của dữ liệu dùng chung là của site, không đếm. `Shallow` của một
  object mượn khoá là các giá trị của nó; `Copy`, `Merge`, `Extend` đếm phần object lớn thêm, kể cả các khoá nó chép
  thành của riêng nó khi thêm khoá;
- thêm khoá vào một object đang mượn thì khoá và giá trị có cùng chỗ cho các khoá sau (chỗ `Copy` để sẵn, hay gấp đôi),
  không cấp phát lại ở khoá kế tiếp;
- năng lượng: một khoá chép khi mượn 2 đơn vị, một khoá ghi 25 như trước.

Đo xen kẽ 10 lượt với bản trước lát này (`BenchmarkSpread`: mẫu, thêm khoá, cập nhật trạng thái; `BenchmarkSpreadWide`:
dòng 12 cột JSON.parse dựng, thêm một khoá và thay một khoá): Spread 862 → 359 µs (−58%), 7842 → 1642 lần cấp phát;
SpreadWide 2864 → 1354 µs (−53%), 11166 → 5166 lần. Fib, Calls, Logic, Make, Text, Boot lệch trong ±3%, và đổi chiều ở
lần đo thứ hai (12 lượt: bản mới nhanh hơn 2–5% cả bốn), nên đó là nhiễu. Năng lượng so với thời gian (`work_test.go`, 2000
khoá): `{ ...o }` 0,73×, `Object.assign({}, o)` 0,9×, `{ x: 1, ...o }` 1,2×, `{ ...o, x: 1 }` 2,1×. Ca cuối chép 2000 khoá và
chỉ mục của chúng khi thêm khoá, mà năng lượng chỉ tính phần byte của chỗ chép ấy; nó vẫn dưới trần 4× của test. Trong
SpreadWide, chép khoá ấy là 4,5% thời gian, phần lớn còn lại là JSON.parse dựng dòng.

Test: `shape_test.go` (bản chép mượn khoá của literal và của object dựng từng khoá, 2 tới 12 khoá; thêm, xoá ở mỗi bên
không tới bên kia; ghi vào object đã có khoá, đóng băng, dùng chung; tám goroutine chép dữ liệu dùng chung, `-race`;
`Holding` đếm khoá một lần, không đếm khoá của dữ liệu dùng chung; `Shallow`); mười một ca `{ ...o }` trong
`language_test.go` so với node (thứ tự chép, khoá trước `...`, hai `...`, khoá trùng sau `...`, khoá dạng chỉ số, mảng,
chuỗi, null, object dài 12 khoá, trạng thái chép qua từng vòng), hai lỗi chỉ vào `...`; `TestSpreadAllocates` (ngân sách
1700); `work_test.go` đo năng lượng của cả hai đường. Đối chứng đều đỏ: không mượn, không chép khi thêm, khi xoá, dữ liệu
dùng chung không có Shape, Shape không phải của lượt chạy, FreezeShared để Shape là của lượt chạy,
`Holding` bỏ khoá của Shape, `Shallow` đếm khoá mượn, `Extend` dựng object tạm, `Copy` không để chỗ, `own` không để chỗ,
bản chép tính giá như ghi từng khoá. Ca thứ tự chép đỏ trên bản trước lát này. Chưa làm: `{ ...o, a }` thêm khoá vào một
object 12 khoá chép cả chỉ mục (một chỉ mục dùng chung cộng phần đuôi sẽ bỏ được, mà nó chỉ 4,5% của SpreadWide).

**JSON.parse dùng chung khoá (owner 24/11/2026; `core/javascript/json.go`, `value.ObjectOf`, `value.Value.Shape`):**
trước đây mỗi object JSON.parse đọc được dựng bằng `Set` từng khoá: mảng khoá và mảng giá trị lớn dần (4, 8, 16), và từ
khoá thứ 9 mỗi object một chỉ mục riêng, dù 200 dòng của một kết quả có cùng 12 cột. Bây giờ:
- khoá và giá trị của các object đang đọc gom ở hai ngăn của bộ đọc (object trong ở sau object ngoài); đọc xong một
  object thì dựng nó một lần, đúng kích thước (`ObjectOf`; khoá trùng thì dựng bằng `Set` như JavaScript);
- một lần đọc nhớ khoá của các object nó đã dựng, theo khoá đầu và số khoá. Object đầu tiên giữ khoá của riêng nó; khi
  object thứ hai có đúng các khoá ấy, cùng thứ tự, khoá của object đầu thành một Shape (`Value.Shape`, chỉ mục của nó
  dùng lại), và từ đó mọi object cùng khoá mượn Shape ấy. Object có khoá không lặp lại không tốn thêm gì;
- mỗi loại (khoá đầu, số khoá) nhớ 4 bộ khoá, để hai loại xen nhau (chủ và danh mục của một dòng, cùng `{ id, … }`) không
  đuổi nhau; quá 256 loại thì quên hết và nhớ lại từ đầu, nên một văn bản muôn kiểu object không giữ một bộ nhớ đệm lớn;
- bộ đếm byte đếm khoá của một object chỉ khi nó giữ khoá của riêng nó: các dòng mượn chung là phần đầu và các giá trị,
  như `Holding` thấy;
- năng lượng: một byte JSON.parse đọc từ 4 xuống 2 đơn vị. Các dòng cùng khoá giờ 3 ns/B (trước 7,5), một mảng số
  9 ns/B; tỉ số với thời gian thành 0,67× và 1,2× (với giá 4 thì các dòng 0,23×, sát ngưỡng 0,2 của test).

Đo xen kẽ 10 lượt với bản trước lát này: Parse (200 dòng có hai object con và một mảng, đọc 5 lần) 2286 → 1995 µs
(−13%), 16593 → 11683 lần cấp phát; SpreadWide (200 dòng 12 cột) 1241 → 746 µs (−40%), 5166 → 3185 lần. Boot, Calls, Fib
chậm 2–4% ở cả hai lần đo, dù không chạm code nào của lát này: vòng lệnh `execute` giống hệt (19 776 byte) nhưng bắt đầu
ở byte 0 của một dòng 64 byte thay vì byte 32. Đệm tạm vài byte vào code JSON cho nó về byte 32 thì cả ba ngang hoặc
nhanh hơn bản trước (12 lượt: Boot −2,8%, Calls −1,1%, Fib −0,8%), nên đó là chỗ đặt mã máy; phần đệm đã gỡ. Giờ mỗi
object còn hai lần cấp phát (phần đầu, mảng giá trị); phần lớn còn lại của Parse là GC và `JSON.stringify` dựng text.

Test: mười ca JSON.parse so với node trong `globals_test.go` (hai loại xen nhau, khoá trùng trong một loại đã nhớ, ở
object nhỏ và object từ 9 khoá, khoá dạng chỉ số, năm bộ khoá một loại, object lồng, reviver xoá khoá trên các dòng
mượn, mười khoá); `parse_shape_test.go` (các dòng mượn khoá của dòng đầu, 3 và 12 cột; ghi ở một dòng không tới dòng
khác; hai loại xen nhau mỗi loại mượn trong loại của nó; object có khoá không lặp lại không có Shape; bộ đếm byte khớp
cái cây giữ, cả dòng cùng khoá lẫn một object 2000 khoá); `TestParseAllocates` (ngân sách 12000); `JSON.parse(rows)`
trong `work_test.go`. Đối chứng đều đỏ: không mượn, không nhớ, object đầu không mượn Shape của nó, so chỉ khoá đầu và số
khoá, một bộ khoá mỗi loại, không thấy khoá trùng (hai đường), không đếm khoá của riêng object, đếm khoá của dòng mượn,
giá trị không chép ra khỏi ngăn của bộ đọc.

**Mảng JSON.parse đúng kích thước (owner 24/11/2026):** mảng gom phần tử ở cùng ngăn giá trị của bộ đọc, đọc xong thì
dựng một lần bằng một bản chép đúng kích thước (`value.Array`). Trước đây mỗi mảng `Push` từng phần tử: `["a","b"]` ba
lần cấp phát, và mảng lớn giữ chỗ thừa của lần nới cuối (tới một phần tư), mà bộ đếm byte không thấy vì nó đếm độ dài.
Một mảng chỉ gom giá trị, không gom khoá, nên mỗi object nhớ chỗ bắt đầu của từng ngăn riêng (`keyStart`, `itemStart`).

Đo xen kẽ (14 lượt): Parse 11683 → 10668 lần cấp phát, thời gian ngang (−0,4%; một lần cấp phát 32 byte mỗi mảng nhỏ là
khoảng 1% của nó, không phải 5% như tôi ước: đó là cả `Push`); SpreadWide, Make, Boot ngang. Một mảng 100 000 số: −5,9%
(8 lượt), dù chép thêm một lần ở cuối, vì thêm vào ngăn rẻ hơn `Push`; 32 với 31 lần cấp phát. Vòng lệnh `execute` ở
cùng chỗ trong hai bản (`go tool nm`), nên số đo không lệch vì chỗ đặt mã máy.

Test: ba ca so với node (object trong mảng trong object có khoá trước nó, mảng lồng và rỗng, reviver làm lỗ),
`TestParseArraysExact` (101 mảng, không quá hai lần cấp phát mỗi mảng; mảng đọc ra thêm phần tử được, không chạm mảng
khác), ngân sách `TestParseAllocates` 11000. Đối chứng đều đỏ: object và mảng chung một chỗ bắt đầu, mảng dựng trên cả
ngăn, mảng không trả chỗ, mảng giữ chính ngăn thay vì bản chép, mảng lớn dần từng phần tử.

**Route (owner 24/11/2026, chốt sau khi bàn; thay mục 3–4 của docs/KITWORK_ROUTER.md cho engine3):** `router.work` là
một bản thiết kế như `application.work`: chạy file chỉ dựng một giá trị, engine đọc cái file `return`.

```js
import { router } from "router";
import { signedIn, staff } from "app/core/session.work";

const route = router({ meta: { title: "Sản phẩm" } });

route.limit({ rate: 300, per: "1m" }).guard(signedIn).all("*");   // mọi method, thư mục này và mọi thư mục con
route.guard(staff).methods("post", "delete");                      // các method ấy, chỉ thư mục này

route.cache("5m").get((context) => context.json({ items: [] }));    // luật đứng trước một cửa: chỉ cửa ấy
route.limit({ rate: 20, per: "1m" }).post((context) => context.status(201).json({}));
route.delete((context) => context.status(204).text(""));

return route;
```

- Thư viện `"router"`, không tiền tố; `router({ meta })` dựng route (thay `router.meta(…)`); file không trả `router(…)` là
  lỗi boot. Thư mục chỉ có `content.ui` không cần `router.work`.
- Hai chữ kết thúc một chuỗi luật, cùng luật đối số (tên method chữ thường, hay `"*"` cho mọi method): `all(…)` là thư mục
  này và mọi thư mục con, `methods(…)` là chỉ thư mục này. Cả bốn tổ hợp viết được: `all("*")`, `all("get")`,
  `methods("*")`, `methods("post", "delete")`. Không đối số, `"*"` cùng tên khác, tên lặp, chữ hoa, tên lạ là lỗi boot chỉ
  sang dạng đúng; `all()` không phải `all("*")`.
- `all` của Kitwork là cả nhánh, khác `all` của Express ("mọi method"): ghi một câu ở spec, và engine sẽ in chuỗi luật
  thực sự áp cho mỗi thư mục, cả luật thừa hưởng từ cha (đề xuất, chưa làm).
- Luật đứng trước cửa (`guard`, `limit`, `cache`) chỉ của cửa ấy; `cache` chỉ trước một cửa GET. Một chuỗi không có chữ
  kết thúc (`route.guard(x);`) là lỗi boot chỉ sang `.all("*")` hay `.methods("*")`. Khác `app.website().guard(fn)`, ghi ngay.
- HEAD đi theo GET. Luật của thư mục áp cả cho trang `content.ui` và file tĩnh của các thư mục nó phủ.
- `route` sửa được trong lúc boot, đóng băng sau boot (như `website`): khác `docs/host/value.go` ("trả bản sao"), sửa ở
  đó khi chốt spec.
- Thứ tự chạy: luật `all` của các thư mục cha, từ gốc xuống; rồi các luật của chính thư mục (`all` và `methods`, theo thứ
  tự viết); rồi luật đứng trước cửa; rồi cửa. Trong một chuỗi, `limit` và `guard` theo thứ tự viết.
- Còn bàn sau: một chữ cho câu trả lời rỗng (`context.status(204).text("")` đọc như gửi chữ; đề xuất `empty()`).

**Cửa website, lát 1 (owner 24/11/2026; `host/website.go`, `host/request.go`):** engine3 trả lời request thật lần đầu.
`host.LoadWebsite(booted.Blueprint)` đóng băng bản thiết kế (trước khi request nào chạy), đọc `listen`, các `guard`,
`success`, `error`, `finally` (mỗi trường sai một lỗi theo đường dẫn của nó), và ghi chú mỗi chữ chưa phục vụ:
`certificates`, `ssl` (mọi cổng nói HTTP thường), `routes`, `memory`, `energy`, `concurrency`, `warm`. `Open` mở các
socket của `listen` (listen.go), `Serve` phục vụ, `Shutdown` đóng. Mỗi request (docs/KITWORK_APPLICATION.md mục 5,
docs/host/request.go, decision.go):
- chuỗi guard chạy trên một lượt chạy riêng, `HostMemory` (64 MiB) và `HostEnergy` (10 000 000), một lượt cho cả
  chuỗi; trần này là đề xuất mới, bản thiết kế chưa có chữ nào đặt nó;
- `context.request`: `method`, `url`, `path`, `search` ("?…" hay ""), `hostname` (chữ thường, không cổng), `ip` (không
  cổng, IPv4 viết như IPv4), `secure` (đến qua TLS), `header(tên)` (null khi không có), `query(tên)` ("" khi không có),
  `cookies`, và các khoá guard đặt. Đọc một khoá không có là lỗi nêu các khoá, kèm cách đặt nó bằng `context.next`;
- quyết định: `context.next()`; `context.next({ khoá })` (khoá chưa có, giá trị là dữ liệu: chép từng tầng rồi đóng
  băng, dữ liệu dùng chung giữ nguyên; hàm, struct, lỗ, sâu quá 64 tầng là lỗi); `context.error(lý do)` là 403,
  `context.redirect(url)` là 302; `context.status(n)` chỉ nhận 3xx rồi `redirect`, 4xx rồi `error`. Lý do tới khách
  thành chữ thuần; địa chỉ chuyển hướng có ký tự điều khiển (một dòng mới là chèn header) là lỗi;
- guard ném, hay trả thứ khác một quyết định, là 500: khách chỉ nhận "Internal Server Error", engine viết lỗi đầy đủ
  (`file:dòng:cột`) vào `Website.Log`, `context.response.error` mang `code`, `message`, `at` (của guard không trả quyết
  định: chỗ của chính guard ấy);
- gửi xong (flush) mới chạy đúng một trong `success` (chuyển hướng) hay `error` (từ chối: mã `REFUSED`, `at` null; lỗi
  của code), rồi `finally`. Chúng đọc `context.response` (`{ status, error }`, đã đóng băng), không quyết định gì; một
  hàm ném thì engine viết lỗi ấy, các hàm sau vẫn chạy; chuỗi guard đã hết năng lượng hay bộ nhớ thì chúng có lượt mới;
- request qua hết guard được 501 ("serving a site comes in the next slice"), mã `NOT_IMPLEMENTED`, tới lát 2 (site).

`context`, `context.request`, `context.status(n)` trả lời khoá và chữ thẳng (`runtime.Direct`), không qua reflect.
`Program.Console` đặt nơi `console.log` của application.work và các file nó import viết (nil là os.Stdout), cả trong
các hàm của bản thiết kế sau đó: test đọc được dòng mà `error`, `finally` in.

Sửa kèm: dữ liệu giả lập của test (`testdata/blueprint/host/system.work`) để dòng tên miền `shop.vn` trước dòng tenant,
cùng `identity`, nên `(tenants) => tenants.identity === …` của guard gặp dòng tên miền (status `confirmed`) và từ chối mọi
request: boot không thấy vì guard chưa chạy bao giờ. Dòng tenant giờ đứng trước, thêm `cu.vn` (tenant tạm dừng) và
`moi.vn` (chưa xác nhận).

Đo (`test/host/bench_test.go`, httptest trong tiến trình, một luồng): qua cả năm guard rồi `error`, `finally` (hai
`console.log`) 11,2 µs, 87 lần cấp phát; guard đầu chuyển sang https 6,3 µs; guard thứ tư từ chối 12,9 µs; riêng
httptest 2,4 µs. Phần lớn là các guard (30%, trong đó `rows.find` của dữ liệu giả lập 7%), các hàm sau khi gửi (9%) và
GC (21%; mỗi request một `runtime.New` với ngăn xếp 8 KB: dùng lại máy là việc sau). Spec mục 12 đo engine2 (chuỗi bốn
guard, bảng 286 tên miền trong bộ nhớ, không có hàm sau khi gửi) 7,66 µs: không cùng phép đo, nên không so. Bench của VM:
cùng có `net/http` trong binary thì thêm `Direct` không chậm gì (12 lượt: Logic −3,1%, Calls −0,6%, Fib −0,3%); so với
binary chưa có `net/http`, Logic lệch +3%, vì mọi hàm dời chỗ.

Test (`test/host/website_test.go`): năm guard của bản thiết kế trả lời đúng tám ca (http sang https, IP bị chặn, tên
chưa đăng ký, chưa xác nhận, tenant tạm dừng, thiếu User-Agent, đường dẫn của máy quét, qua hết) và in đúng dòng của
`error`, `finally`; một socket thật trên 127.0.0.1; các khoá của request; `next` (đặt, chép, khoá, khoá đã có, hàm, hai
lần); mọi quyết định và mã sai; các hàm sau khi gửi (sau khi gửi, một hàm ném, guard đọc response); năng lượng của một
request, request sau sạch, guard ghi vào biến nó bắt; 16 goroutine cùng lúc (`-race`); lỗi lúc nạp. 23 đối chứng đều
đỏ. Chưa làm: TLS (`certificates`, `ssl`), `routes` và site (lát 2), `memory`, `energy`, `concurrency`, `warm`; chưa có
lệnh nào chạy cửa này (`cmd/root` chờ owner).

**Cửa website, lát 2: site (owner 24/11/2026; `host/site.go`, `host/limit.go`, `blueprint/router`):** request qua hết guard
của host tới site thật; 501 của lát 1 không còn. Mốc nghiệm thu: `https://shop.vn/` của bản thiết kế docs/host/blueprint
đi qua năm guard, `routes` chọn `apps/03dt/shop.vn`, `memory` đọc `tenant.memory` ("128mb"), và `router.work` của nó
(bản chép, viết theo route đã chốt) trả "Xin chào từ shop.vn của tenant 03dt".
- Chuỗi host đi tiếp sau guard: `routes` (chuỗi, hay hàm) là thư mục site tính từ thư mục manifest, có một thư mục cha
  (gốc app, nơi `app/…` của import bắt đầu) khác chính thư mục manifest, không ra ngoài nó (500 kèm lý do, ở chỗ của
  `routes`); `memory` (`"64mb"`, `value.ByteSize`) và `energy` (số) là trần của lượt chạy của site, không viết thì
  `HostMemory`, `HostEnergy`. Website không có `routes`: một ghi chú lúc nạp, request qua hết guard là 404. Trong
  `routes`, `memory`, `energy`, context chỉ đọc request.
- Site biên dịch ở request đầu tới nó, một lần dù nhiều request cùng tới (`sync.Once`), rồi giữ, kể cả lỗi biên dịch;
  thư mục không có thì không giữ gì (routes đọc từ request không làm phình bộ nhớ), và hỏi đĩa chỉ lần đầu. Câu hỏi 6
  của spec (biên dịch lúc boot) còn mở: bản thiết kế chưa nêu các site, nên engine không tự quét thư mục nào. Mỗi thư mục
  là một đoạn đường dẫn (thư mục bắt đầu bằng `.` hay `_` không phải, trừ `.well-known`); `{…}` là lỗi nói lát sau. Mọi
  file của site import được `"router"`; `app/…` tính từ gốc app, `./`, `../` từ thư mục của file, không ra ngoài gốc app;
  `kitwork/…` là lỗi biên dịch. `console.log` của site có đầu dòng `[tên site]`. Xong thì mọi route và export được đóng
  băng (`FreezeShared`): một handler ghi vào biến nó bắt, hay gọi `route.get(…)` lúc request, là lỗi.
- Route (`blueprint/router`): các chữ kiểm đối số ngay khi gọi (lỗi mang `file:dòng:cột` của dòng viết sai), chuỗi không
  có chữ kết thúc kiểm khi nạp ("chain 2, route.guard(…), has no end"). Tìm thư mục theo đường dẫn (gạch chéo cuối, lặp
  không đổi gì); thư mục không có, hay không có cửa nào, là 404 (`NOT_FOUND`); OPTIONS là 204 kèm `Allow`; method không có
  cửa là 405 kèm `Allow` (`METHOD_NOT_ALLOWED`), sau các luật; HEAD đi theo GET, không có thân.
- `limit`: cửa sổ cố định, một hạn mức mỗi client mỗi luật (`all`, `methods`: theo IP, cả nhánh chung một bộ đếm; trước
  một cửa: theo IP và đường dẫn), quá là 429 kèm `Retry-After` (`TOO_MANY_REQUESTS`); một luật nhớ tới 100 000 client,
  quá thì quên cửa sổ đã hết, vẫn quá thì quên hết. `cache` được đọc và kiểm (`value.TimeSpan`), chưa giữ câu trả lời nào:
  một dòng log lúc biên dịch site.
- Context của site: guard nói `next`, `error`, `redirect` hay trả lời luôn (`text`, `json`, `status(n).…`); cửa trả lời
  bằng `text`, `json`, `redirect`, `status(n).…` (`next`, `error` trong cửa là lỗi nói của ai). Code của site chuyển hướng
  trong cùng host: một đường dẫn `"/…"`. `context.json` viết như `JSON.stringify`; thứ không có JSON là lỗi. Câu trả lời
  của site là thành công (`success`), cả khi mã là 4xx; 404, 405, 429 của engine và lỗi của code là `error`.

Đo (`HostChainThrough`, tốt nhất trong 6 lần, máy lúc đo chậm hơn lúc đo lát 1 khoảng 15%: mốc httptest 2,8 so với 2,4
µs): qua năm guard tới cửa của site rồi `finally` 14,9 µs, 99 lần cấp phát; chuyển hướng 7,5 µs; từ chối 15,5 µs. Bản
đầu hỏi đĩa (`os.Stat`) mỗi request trước khi tra site đã biên dịch: 20,5 µs; nay chỉ lần đầu.

Test (`test/host/site_test.go`): luật của một route (all đi xuống, methods không, luật trước cửa, limit của nhánh chung
một bộ đếm mỗi máy, HEAD, 405, OPTIONS, 404, thư mục `.`, `_`, gạch chéo cuối); các câu trả lời của site (guard trả lời,
redirect trong host, status, json, các lỗi của cửa, console có đầu dòng); mười bảy lỗi của một `router.work`, mỗi lỗi
một chỗ; `routes`, `memory`, `energy` (ra ngoài, không gốc app, không có, trả số; trần bộ nhớ, năng lượng của site; ghi
vào biến bắt được, ghi lên route); 16 goroutine cùng tới một site lần đầu, biên dịch một lần (`-race`). 24 đối chứng đều
đỏ. Gỡ kèm: một chỗ kiểm cửa trả `next` hay từ chối, không tới được (allows chặn trước), không đối chứng nào làm đỏ được.
Chưa làm: đoạn động `{id[number]}`, `content.ui`, file tĩnh, thân request (`json()`, `form()`), `cache` thật, KitDB (lát
3: trang `don-hang` của bản thiết kế), chuỗi luật thực sự áp cho mỗi thư mục (đề xuất), `concurrency`.

**Database, lát 3: KitDB (owner 24/11/2026: "vâng làm lát 3 KitDB cho engine3 đi"; package `database`: `schema.go`,
`connection.go`, `translate.go`, `statement.go`):** engine3 đọc bảng thật qua sqlitewire. Mốc nghiệm thu: ba guard của
bản thiết kế đọc `system` (host/system.work thật, `connection().schema("system").tables({ domains, tenants, banned })`,
thay bản giả lập), và `https://shop.vn/don-hang` trả các đơn `"moi"` của tenant 03dt, mới nhất trước, bằng JSON
(`db.where(…).descending(…).list(20)`).
- Thư viện gọi bằng tên: `"schema"` (struct, id, string, integer, float, decimal, timestamp, date, boolean, choice; luật
  của cột được ghi lại, lát này chỉ dùng loại, số chữ số lẻ, nullable; mỗi luật trả một cột mới, cột cũ không đổi) và
  `"database"` (`connection()`, `.schema("tên")` một lần, `.tables({ … })`). Site: mọi file import được cả hai;
  `connection()` là `<gốc app>/.database/application/kitwork.db`, `.schema("x")` là `x.db` cạnh đó. Host: các file
  application.work import thấy cả hai (trước: chỉ các file khác), `connection()` tính từ thư mục application.work.
  `connection("tên")` (danh mục của host) là lỗi nói lát sau.
- Câu lệnh: `where`, `ascending`, `descending` không chạy gì, mỗi chữ trả một câu mới (câu cũ không đổi, giữ trong biến
  viết tiếp được); `list()` đọc 1000 dòng nhiều nhất (hơn là lỗi nói viết `list(n)`), `list(n)` từ 1 tới 10000,
  `list(fn)`, `first(fn?)` (null khi không có), `count(fn?)`. Bảng là tên tham số của hàm (`(orders) => …`); một câu
  đọc một bảng. Thứ tự: các bước theo thứ tự viết, rồi khoá `id()` (không có thì rowid).
- Điều kiện dịch sang SQL từ cây cú pháp của hàm (`compiler.Function.Syntax`, mới), một lần mỗi hàm mỗi Handle (nhớ trên
  Handle: một Handle dựng trong request đi cùng bản dịch của nó), không đọc lại nguồn, nên một hàm giữ trong biến dùng
  được. Dịch được: `&&`, `||`, `!`, `===` (IS: so được null), `!==`, `<`, `<=`, `>`, `>=`, null, chữ, số, true/false,
  một cột boolean đứng một mình; một biến hàm bắt và đường thuộc tính sau nó (`wanted.status`; `Capture.Name`,
  `Closure.Captured`, mới) là một tham số `?`, đọc lúc câu chạy. Thứ khác là lỗi TYPE ở chỗ của nó trong hàm
  (`file:dòng:cột`, không phải chỗ của lời gọi `.where`): cột không có (kể các cột), bảng không có (kể các bảng), giá
  trị tính trong điều kiện, thân là khối, tên không phải biến, so thứ tự một cột nullable, boolean hay choice.
- Giá trị đổi theo cột như engine2 lưu, để đọc được cùng file: decimal là số nguyên × 10^scale, timestamp là chữ UTC tới
  mili giây (đọc ra số mili giây), boolean là 0/1; sai loại là lỗi TYPE ("id is a string, compared with column id (id):
  write a number"). Các dòng đọc ra mượn chung khoá của bảng (Shape), theo thứ tự struct.
- Lỗi DATABASE (mã mới của tập đóng, tên của engine2): file chưa có ("migrate makes it from the structs"), bảng hay cột
  struct khai mà file thiếu ("migrate adds it"). Tên bảng, cột viết trong dấu huyền: trong ngoặc kép, SQLite đọc một cột
  không có thành một chuỗi và câu chạy không lỗi (bản đầu: ca thiếu cột trả 200). engine2 kiểm file với struct lúc boot
  nên không gặp; engine3 chưa có bước ấy.
- Mỗi file một `sqlite.Database` mỗi tiến trình (`database.Files`: mở ở câu đầu đọc nó, `Booted.Close` đóng); câu chạy
  trên một bản chụp đọc, dưới context của request (`runtime.Options.Context`, `Machine.Context()`, mới). Mỗi dòng tốn 20
  đơn vị năng lượng và số byte nó giữ; các dòng đang dựng được bộ đếm thấy (`Keep`).

Đo (3 lần, `-benchtime 2000x`): qua năm guard (ba câu đọc KitDB) tới site rồi `finally` khoảng 160 µs, 350 lần cấp phát
(lát 2, dữ liệu trong bộ nhớ: 14,9 µs, 99 lần); trang don-hang (thêm một câu, hai dòng, JSON) 260–300 µs. Profile: hơn
nửa là `Database.Read` của sqlitewire, mỗi lần mở một giao dịch đọc và chuẩn bị lại hai câu SQL (câu `max(rev)` của bản
chụp, rồi câu của ta).

Cache câu đã chuẩn bị của sqlitewire (owner 24/11/2026: "vâng làm cache sqlitewire đi"; `sqlitewire/sqlite/prepared.go`):
một câu `Snapshot.Query` đã chạy được chuẩn bị sau lần đọc ấy và giữ trên mỗi kết nối đọc nó chạy (`Options.Statements`,
128 câu, -1 là không giữ). Câu `max(rev)` vẫn chạy mỗi lần đọc (cache luôn nó): revision ghi vào danh sách bản chụp đang mở,
chốt không cho search nén qua một lần đọc còn mở. Đo xen kẽ 8 vòng hai bản test của engine3 (giữ câu / không giữ), trung
vị: năm guard 173 → 93,5 µs, trang don-hang 245 → 153 µs; một `Read` của sqlitewire một mình 40,5 → 20,4 µs.

Cache truy vấn của sqlitewire (owner 24/11/2026: "vâng làm cache truy vấn sqlitewire đi"; điều kiện thứ nhất của
`docs/KITWORK_APPLICATION.md` mục 12; `sqlitewire/sqlite/querycache.go`): `Database.Query(ctx, sql, args…)` giữ kết quả
theo (câu, đối số) và đưa lại tới khi một commit đổi một bảng câu ấy đọc. `list`, `first`, `count` của engine3 đi qua nó
(`database/statement.go`); các dòng dùng chung, mỗi lượt chạy dựng giá trị của riêng nó và vẫn trả năng lượng, byte từng
dòng. Bảng một câu đọc là SQLite nói (EXPLAIN, kể bảng dưới view và bảng của một index); câu gọi hàm ngoài danh sách tất
định (random, datetime, …), đọc bảng ảo, tạm hay gắn thêm thì không giữ. Người ghi đánh số mỗi commit theo các bảng hook
thấy đổi (cả bảng lịch sử không theo dõi), đánh dấu "đang đổi" trước COMMIT và đánh số sau: bản đầu đánh số sau COMMIT
thôi, và test nhiều goroutine bắt được một lần đọc lùi (10057 rồi 10056): COMMIT cho bản chụp mới thấy trước khi số kịp
tăng. Ghi từ tiến trình khác: PRAGMA data_version, thấy trong 1 giây. Trần 8 MiB mỗi file (`Options.QueryCache`, -1 tắt).
Đo xen kẽ 8 vòng, hai lượt (có cache / không, cùng cache câu đã chuẩn bị), trung vị lượt hai: năm guard 137 → 33,5 µs
(350 → 126 lần cấp phát), trang don-hang 214 → 58,7 µs; lượt này máy chậm hơn lượt đo cache câu khoảng 1,5 lần (cùng
bản không cache: 93,5 µs khi ấy). Trong sqlitewire một lần trúng cache ≈ 170 ns. Profile năm guard: đường database của
engine3 còn ≈ 3 µs mỗi request; phần còn lại là VM, GC của các lần cấp phát, httptest.

Test (`test/host/database_test.go`): mốc nghiệm thu (don-hang, khoá đúng thứ tự); 17 câu đọc (điều kiện, biến bắt được,
decimal, timestamp, boolean, null, `||`, first không có, count, thứ tự, bảng thứ hai, câu không đổi khi viết tiếp, hàm
giữ trong biến); 16 câu từ chối, mỗi câu đúng mã và lời báo, lỗi dịch đúng chỗ trong hàm; `list()` quá 1000 dòng; năng
lượng và bộ nhớ của các dòng; 16 goroutine cùng đọc một file (`-race`). Các test của lát 1–2 chạy trên một bản chép của
testdata/blueprint có hai file database, tạo bằng chính sqlitewire (câu của engine không tạo file hay bảng). 10 đối
chứng đều đỏ (decimal ghi, decimal đọc, `===` là IS, timestamp, năng lượng, byte, dấu huyền, trần list(), câu không đổi,
thư viện của site). Chưa làm: ghi, migrate, kiểm file với struct lúc boot, join, includes, matches,
`connection("tên")`. Câu hỏi cho owner: dữ liệu host thật ở `.database/system/kitwork.db`, spec cho
`connection().schema("system")` là `.database/application/system.db`.

**File tĩnh, lát 4 (owner 24/11/2026: "vâng làm lát 4 file tĩnh qua chuỗi host đi"; `host/static.go`, `host/site.go`,
`host/website.go`):** một site phục vụ nguyên dạng `assets/`, `.well-known/` và danh mục file ở gốc (robots.txt,
favicon.ico, …), luật lấy từ engine2 (`host/static.go`, `host/prewarm.go`), gọn lại. File tĩnh đi sau chuỗi host như mọi
request (guard, routes, memory, energy), rồi các luật của thư mục phủ nó: `all` của các thư mục từ gốc tới thư mục của
file, `all` và `methods` của chính thư mục ấy (luật "Route": luật của thư mục áp cả cho file tĩnh); thư mục `_…` không có
luật riêng; không luật nào áp thì không dựng lượt chạy nào. GET và HEAD đọc file; file không có, hay method khác, đi tới
các cửa. Câu trả lời: loại của engine (không đoán), nosniff, ETag yếu từ giờ ghi và cỡ, `http.ServeContent` (304, Range,
HEAD), một năm `immutable` chỉ khi tên mang đầu SHA-256 của byte (kiểm bằng byte, nhớ theo file, cỡ, giờ ghi), còn lại
một giờ; `context.response.status` là mã ServeContent viết. Không file nguồn, file chấm hay file ngoài danh sách nào ra
ngoài: tên có `\`, `:`, đoạn rỗng, `.`, `..`, đoạn kết thúc bằng chấm hay dấu cách, tên ngắn của Windows, `.work`, `.ui`,
`*.kitwork.*` là không phải file; mọi file mở bằng `os.OpenInRoot` trong thư mục site (link, junction ra ngoài không theo).
Đối chứng của chốt `\` tìm ra một đường thật: không có nó, `/assets/sub\..\..\.env` đọc `.env` của site.
- `warm` (true, false, hay `(context) => boolean` nhận `context.site` { hostname, folder }; giá trị khác là lỗi lúc nạp):
  hỏi khi site biên dịch, ở request đầu tới nó (câu hỏi 6 còn mở); hàm ném hay trả thứ khác boolean thì engine viết lý do
  vào Log và site không giữ ấm (warm chỉ đổi tốc độ, không đổi câu trả lời; tôi chọn, chờ owner). Site giữ ấm giữ file
  trong RAM, mỗi file tới 256 KB, cả site tới 32 MB, so với đĩa nhiều nhất mỗi giây một lần (`Website.StaticCheck`), một
  dòng log "warm: N files in memory". Site không giữ ấm mở file ở mỗi request, đóng sau khi gửi (cả khi luật từ chối).
- Đo xen kẽ 8 vòng với engine2 (`archive/v0.2`, cùng file, `httptest`, trung vị): giữ ấm một luồng 19,9 µs (131 lần cấp
  phát) so với 4,81 µs (28 lần); giữ ấm mười sáu luồng 10,6 so với 3,93 µs; không giữ ấm 127 so với 99 µs. Mục 12 ước
  10–11 µs một luồng khi có cache truy vấn: luật "không chậm hơn" chưa đạt (chậm 4,1 lần, mục 12 đã đoán 2 lần). Profile:
  gửi file ≈ 1 µs; phần lớn là VM chạy năm guard, trong đó các câu database (dựng câu, khoá cache, dựng dòng) khoảng một
  phần ba; GC khoảng 30% CPU (≈ 22 KB mỗi request).
- Test (`test/host/static_test.go`; mốc nghiệm thu thêm `assets/app.css`, `robots.txt` của shop.vn và một IP bị chặn hỏi
  file): file và header, 304, Range, HEAD (giữ ấm và không); 23 đường không được ra file; junction của Windows; guard của
  host trước file; file không có đi tới cửa; luật thư mục (all đi xuống, methods không, `_…`); giữ ấm (đọc RAM trước lần so,
  bản mới sau, xoá thì 404, site không giữ ấm đọc đĩa), file quá 256 KB, warm là hàm, lỗi của warm; handle đóng sau khi
  gửi và khi luật từ chối (mở độc quyền của Windows nói được, xoá thì không: Go mở file có chia sẻ xoá); 16 goroutine
  (`-race`). 21 đối chứng đều đỏ. Chưa làm: tổng 32 MB chưa có test (engine2 thử bằng trường nội bộ), symlink (máy không
  có quyền tạo, test bỏ qua), favicon của router, trang `content.ui`.

## Cách viết `application.work` (owner 24/11/2026)

Owner, 24/11/2026: "tôi còn định hướng application theo kiểu này nữa" (dạng `const app = application(…)`,
`app.<cửa>(…)`, `return app`), "schedule nên kết thúc bằng run()", về lệnh "execute được đó chứ", rồi "tôi gom
application / website ... về đúng 1 chỗ là application thôi?". Spec: `docs/KITWORK_APPLICATION_WORK.md` (repo gốc).
Cách dựng rời trước đó (`const web = website(443)…; return application({ … }).run({ web, db })`) không còn: nó là lỗi
boot nói cách viết mới.

```js
import { application, environment } from "application";
import { https, pick } from "./host/web.work";            // file phụ xuất hàm, không xuất cửa

const env = environment("./.env");

const app = application({ memory: "2gb", timezone: "Asia/Ho_Chi_Minh" });

app.website({ certificates: ".certificates" }).guard(https).routes(pick).listen(443, 80);
app.database({ port: 5445, password: env.require("KITDB_PASSWORD") }).open(".database");
app.schedule().memory("512mb").run(".schedules");
app.command("kitcli").execute({ backup: (context) => backup() });

return app.run();                                          // hay return app.check();
```

Một câu của cửa có ba phần: mở câu bằng cấu hình, chuỗi các chữ của cửa, và **chữ cuối**, một động từ nhận giá trị
chính của cửa, đóng câu, và trả về `app`:

| Cửa | Mở câu | Chữ cuối | Giá trị chính (trường) |
|---|---|---|---|
| website | `app.website()`, `app.website({ … })` | `.listen(443, 80)` (tên thứ hai: `.port(…)`) | các địa chỉ nghe (`Listen`) |
| database | `app.database()`, `app.database({ … })` | `.open(".database")` | thư mục các file database (`Open`) |
| schedule | `app.schedule()`, `app.schedule({ … })` | `.run(".schedules")`, `.run([…])`, `.run(() => […])` | các thư mục có lịch (`Folders`) |
| command | `app.command("kitcli")`, `app.command({ … })`, `app.command("kitcli", { … })` | `.execute({ backup, migrate })` | bảng lệnh (`Handle`) |

- **Chữ cuối không làm gì lúc nạp**: `.listen(…)` không nghe, `.run(…)` không chạy lịch nào. Công tắc duy nhất là
  `app.run()`. Ba chữ `run`, ba nghĩa: `app.run()` bật chương trình, `app.schedule(…).run(…)` đóng câu của cửa lịch,
  `schedule.daily("09:00").run(fn)` trong một file lịch khai một job.
- **Chữ cuối đòi giá trị của nó**: `.listen()`, `.open()`, `.run()`, `.execute()`, `.execute({})` là lỗi ngay ở dòng
  viết. Không viết được một website không cổng, một database không mở gì, một cửa lệnh không lệnh nào.
- **Giá trị chính chỉ có một chỗ, sau chữ cuối**: `app.website(443)`, `app.database(".database")`,
  `app.schedule(".schedules")` là lỗi nói chữ cuối; `listen`, `port` (website), `open`, `run`, `execute` không phải khoá
  của dạng object (`{ listen: 443 }` là lỗi nói chữ cuối). `folders` và `handle` không còn là chữ. Tên chương trình của
  command đứng ở đầu câu: nó không phải thứ `execute` tác động lên.
- **Chữ của cửa nằm trên cửa, không trên `app`**: `app.website(…)` trả về Website, chữ cuối mới trả về `app`. `app` chỉ
  có `memory`, `timezone`, tên các cửa, `run`, `check`. `app.guard(fn)` là lỗi nói chữ ấy của cửa nào.
- **Chương trình không có cổng của riêng nó** (owner 24/11, "Bỏ đi"): `application(8080)`, khoá `listen`/`port` của
  `application({ … })` và `app.listen(…)` của 23/11 là lỗi nói địa chỉ là của cửa nghe nó. Website nghe ở đâu thì đọc
  một chỗ: câu của website.
- **Một câu nối liền cũng viết được**, vì chữ cuối trả về `app`:
  `return application().website().routes("apps/shop").listen(443).database().open(".database").run();`
- **`app.run()`, `app.check()` không đối số, một trong hai, và là dòng cuối**: sau nó không chữ nào ghi được nữa
  (`app.run() is the last line …`), nên `app.run(); return app;` (bản owner đưa) và `return app.run();` cho cùng một bản
  thiết kế. File không xin cái nào là lỗi boot (`end with return app.run(), or return app.check() …`).
- **Desktop, mobile** chưa có câu của mình: `app.desktop(v)`, `app.mobile(v)` giữ giá trị và trả về `app`, như trước.

**Ba luật để không cửa nào ẩn** (đề xuất của spec mục 4, làm theo; `host/doors.go`, trên cây cú pháp, trước khi biên
dịch, nên lỗi có `file:dòng:cột` của chính chữ viết sai):

1. **Cửa chỉ khai ở cấp file.** Tên của app (`const app = application(…)`, tên nào cũng được) chỉ đứng đầu một câu của
   cấp file, hay sau `return`. Nó không vào một hàm, không gán cho tên khác (`const web = app.website()`), không làm đối
   số, không đứng trong `if` hay vòng lặp. `application(…)` viết một lần, ở cấp file, bằng `const`. Tách file thì file
   phụ xuất hàm (file phụ vốn không import được `"application"`).
2. **Một cửa khai một lần.** Lần thứ hai là lỗi nêu chỗ của lần đầu
   (`application already has a website, declared at application.work:5:1 …`).
3. **Một câu phải có chữ cuối.** `app.website({ … }).guard(https);` là lỗi ở đầu câu, nêu chữ còn thiếu
   (`the website is not closed: end the statement with .listen(…)`).

Blueprint giữ cùng các luật lúc chạy, cho chỗ cây cú pháp không thấy và cho Go gọi thẳng: `application.open` từ chối
cửa thứ hai, `Application.Dangling()` kê các cửa chưa đóng (`host/program.go` `shape` hỏi nó sau khi file chạy xong; tắt
luật 3 ở `doors.go` thì boot vẫn dừng, bằng lời báo không có dòng). Một chữ cửa không có (`.lisen(443)`) thì `doors.go`
không phán câu ấy: VM nói chữ nào có.

Test: `test/host/doors_test.go` (39 cách viết sai, mỗi cái một lời báo kèm chỗ; 10 cách viết đúng),
`test/blueprint/blueprint_test.go` (chữ cuối trả về app, cửa khai một lần, run/check, chương trình không có cổng
riêng), `test/host/describe_test.go` (bản thiết kế viết ra), test từ vựng (dưới). 72 đối
chứng (tắt từng luật trong code) đều đỏ. Gỡ kèm: một nhánh kiểm `application(…)` trong hàm trùng với nhánh kiểm tên,
không đối chứng nào làm đỏ được.

Owner chốt 24/11 ("Giữ như đang làm"): câu của database là `app.database({ port, user, password }).open(".database")`,
cấu hình ở đầu, thư mục sau `open`; bản `app.database(".database").open({ user, password, port })` owner viết trước đó
không dùng. Cùng ngày, cùng lời: chữ cuối của schedule và command mang giá trị, `.run(".schedules")` và
`.execute({ … })`; dạng chữ cuối trống (`app.schedule(".schedules").run()`) không viết được. Và "Bỏ đi": cổng ở mức
chương trình (`application(8080)`, `app.listen(…)`, owner 23/11) gỡ hẳn, vì `.listen(…)` của website nay bắt buộc có
địa chỉ, nên cổng của chương trình chỉ còn là chỗ thứ hai phải nhìn. Và "Giữ là lỗi": luật 1 đọc chặt, cửa trong `if`
ở cấp file là lỗi (`if (env.DB) app.database(…)` không viết được); đọc file là biết chắc host mở những cửa nào.

**Bản thiết kế viết ra** (owner 24/11, "Làm đi"; `host/describe.go`): `Booted.Describe()` trả bản thiết kế đã dựng, mỗi
cửa một dòng, để đọc cả chương trình ở một chỗ:

```text
application  application.work:15:13  memory "2gb" · timezone "Asia/Ho_Chi_Minh"
  database   application.work:108:1  open ".database" · listen 5445 · password ••••
  schedule   application.work:96:1   run application.work:102:8 · memory application.work:97:11 · 1 error
  command    application.work:111:1  name "kitcli" · execute backup, migrate, tenants · 1 error
```

- Ba cột: tên, chỗ khai (`Booted.Places`, từ `doors.go`: `file:dòng:cột` của câu; của application là chỗ của lời gọi
  `application(…)`), các giá trị đã ghi cách nhau bằng ` · `, theo thứ tự các trường của `Fields` (giá trị chính của cửa
  đứng đầu). Thứ tự dòng cố định: website, database, schedule, command, desktop, mobile.
- Các trường đọc bằng reflect, nên một trường mới của một cửa có mặt mà không phải thêm gì; chữ của trường là tên JS của
  nó (`jsName`), trừ bốn trường trong `describedWords`: `Guards` là `guard`, `Errors` là `error`, `Folders` là `run`,
  `Handle` là `execute`.
- Một hàm là chỗ của nó; danh sách toàn hàm là số hàm và chữ (`5 guard`); bảng lệnh là tên các lệnh; hàm có sẵn là
  `function`. `password` của database chỉ in `••••`. Cửa vắng không có dòng, trường chưa ghi không có mặt.
- Không kiểm gì: in đúng thứ người viết viết, kể cả giá trị sai, nên dùng được trước bộ nạp. Bản thiết kế Go dựng (không
  qua `application.work`) không có chỗ khai: cột hai trống.
- Hôm nay chỉ `cmd/demo` gọi nó, khi file kết thúc bằng `app.check()`; engine3 chưa có chương trình chính để quyết định
  lúc nào in. Gỡ kèm khi làm: một nhánh "danh sách bí mật" không trường nào đi tới, không đối chứng nào làm đỏ được.

Chỗ tôi tự quyết khi làm, chờ owner (spec mục 8):
- luật 2 và 3 (một cửa một lần, câu phải có chữ cuối) và các lời báo.
- bản thiết kế in lúc nào (chỉ khi `app.check()`, hay cả khi `app.run()`). Hình dạng dòng in của `Describe` thì owner
  đã duyệt 24/11 ("Được, giữ vậy").

**Thư viện `"application"` (owner 23/11/2026):** thư viện gọi bằng tên, không còn tiền tố `"kitwork/"` (máy là gốc,
không có trong mã; `"kitwork/…"` là lỗi biên dịch nói tên mới). Chỉ `application.work` import nó, và nó đưa ra đúng hai
tên (owner 24/11: các cửa gom về application): `application`, và `environment`, môi trường của host
(`host/environment.go`, `host/check.go`; luật của `docs/KITWORK_ENVIRONMENT.md`, bản `file.env` đổi tên).
`import { website } from "application"` là lỗi nói `app.website(…)`:
```js
import { application, environment } from "application";

const env = environment("./.env");                       // const ở cấp cao nhất, đường dẫn viết sẵn, trong thư mục này
const app = application();
app.database({ port: 5445, password: env.KITDB_PASSWORD }).open(".database");   // thiếu: engine báo ở trường password
app.website().guard((context) => env.require("STRIPE_SECRET") ? context.next() : null).listen(443);
```
- `env.KEY` là giá trị đã đổi kiểu (số, boolean ở dạng chuẩn; trong ngoặc luôn là chuỗi), `null` khi không ai đặt (rỗng
  là không đặt). `env.require("KEY")` nói khoá phải có: boot kiểm mọi `require` của file một lần, trước khi chạy dòng
  nào, kể cả trong hàm chỉ chạy lúc request, và báo mọi khoá thiếu trong một lời báo kèm chỗ.
- Biến của hệ điều hành thắng mọi file; nhiều file thì file sau đè file trước theo từng khoá; không có file là một ghi
  chú, không phải lỗi.
- Chỉ đọc theo tên: `env` không chuyền đi, không `env[…]`, không ghi (lỗi biên dịch). Một tham số cùng tên che nó.
- Ghi chú lúc boot: một `env.KEY` không có giá trị (kèm chữ gần đúng: `did you mean KITDB_PASSWORD?`), một file không
  có, một file đặt khoá tên `require`.
- Chưa có: giá trị bí mật (in ra `••••`), chia khoá xuống tenant.


```text
blueprint/
├── application/
│   ├── struct.go           Application: memory, timezone, sáu cửa (vắng: không có cửa ấy); dấu run, check; Fields, Share, Shared, Running, Checking
│   ├── method.go           New, các chữ JS (chỉ ghi), bốn chữ mở câu của một cửa (dựng cửa, trả về cửa), và hai động từ run, check
│   └── helper.go           set, add, open (một cửa một lần), verb, writable (sau run, check không ghi được), Dangling, assign (các dạng của application(…)), keys (mỗi khoá một case; cửa không phải khoá)
├── website/
│   ├── struct.go           Website: listen, certificates, concurrency, warm, ssl, guards, routes, memory, energy, success/errors/finally; app của nó, dấu đã đóng; Fields, Closed, Share, Shared
│   ├── method.go           New(app, …), chữ cuối listen/port (trả về app), và các chữ JS: mỗi chữ chỉ ghi (set) hay thêm vào cuối (add) rồi trả lại Website
│   ├── helper.go           set, add, close (chữ cuối), assign (các dạng của app.website(…)), keys (mỗi khoá một case)
│   ├── context.go          SiteContext + Site (warm) · HandshakeContext + Handshake (ssl) · RequestContext + Request + Response (mỗi request)
│   └── decision.go         Decision: next, refuse, redirect, thứ Go đọc khi guard return
├── schedule/
│   ├── struct.go           Schedule: folders, memory, energy, success/errors/finally; app của nó, dấu đã đóng; Fields, Closed, Share, Shared
│   ├── method.go           New(app, …), chữ cuối run(folders) (trả về app), và các chữ JS (chỉ ghi)
│   ├── helper.go           set, add, assign, keys (mỗi khoá một case)
│   └── context.go          JobContext + Job: name, folder, error
├── database/
│   ├── struct.go           Database: open, listen, concurrency, memory, timeout, user, password, tls, binding, authority; app của nó, dấu đã đóng; Fields, Closed, Share, Shared
│   ├── method.go           New(app, …), chữ cuối open(folder) (trả về app), và các chữ JS (chỉ ghi), port là chữ thứ hai của listen
│   └── helper.go           set, add, assign (các dạng của app.database(…)), keys (mỗi khoá một case)
├── command/
│   ├── struct.go           Command: name, handle, success/errors/finally; app của nó, dấu đã đóng; Fields, Closed, Share, Shared
│   ├── method.go           New(app, …), chữ cuối execute({ … }) (trả về app), và các chữ JS (chỉ ghi): app.command("kitcli") hay app.command({ … })
│   ├── helper.go           set, add, assign, keys (mỗi khoá một case)
│   └── context.go          CommandContext + Invocation: name, error; arguments, options
├── desktop/struct.go       Desktop + Window
└── mobile/struct.go        Mobile + Theme
```

## Luật

- Các trường khai đúng một lần, trong type `Fields` của mỗi cửa (trường xuất ra, kiểu `value.Value`, danh sách là
  `[]value.Value`; ghi chú nói các dạng engine nhận và lúc nó chạy). Struct của cửa giữ nó trong trường có tên
  `fields` (viết thường, không nhúng: không trường nào được đưa lên, package khác không ghi được) và thêm dấu đã nạp.
  Engine đọc bằng `Fields()`, như đang có, không chép: nó chỉ đọc, sau khi bản thiết kế đã đóng băng. Thêm một trường
  là một dòng trong `Fields` và một chữ JS.
- Chữ JS là phương thức chữ hoa mà mọi tham số và kết quả là `value.Value`, tên viết thường chữ đầu (`SSL` là `ssl`).
  Mỗi chữ chỉ ghi, không kiểm: trường một giá trị thì lần sau đè lần trước, trường danh sách thì thêm vào cuối.
  `New(app, options…)` của một cửa (chỉ `application` gọi nó) nhận `app.website()` hay `app.website({ … })`; dạng khác
  là lỗi nêu chúng (`assign`). Thứ quan trọng nhất của cửa (owner 23/11: với website là địa chỉ, với database là mở cái
  gì, với schedule là các thư mục có lịch) nay là giá trị của chữ cuối (owner 24/11), không đứng ở chỗ mở câu; tên
  chương trình của command vẫn ở đầu câu. Database: cổng không có mặc định, không bắt buộc: viết bằng khoá `listen` hay
  `port` (`app.database(5445)` là lỗi dạng). Vắng cổng là không mở cổng
  PostgreSQL, chỉ app của host dùng database, `listen` của chương trình không vào nó (in một dòng, không lỗi: website
  trên 8080 và database nội bộ là một chương trình đúng), và `password` chỉ bắt buộc khi có cổng. Thứ được mở thì bắt
  buộc, không mặc định: câu không có `.open(…)` là lỗi boot (luật 3).
  `.open("127.0.0.1:5445")` mở thư mục tên ấy: engine báo khi nạp rằng nó trông như một địa chỉ. Không có công tắc
  `search` (owner 23/11): MATCH qua cổng chạy mặc định trên mọi bảng có chỉ mục toàn văn, và chỉ mục khai trong chính
  database (cột `.searchable()` của schema, hay `CREATE INDEX … USING search` qua cổng), không trong `application.work`.
  Trần của một câu SQL qua cổng (owner 23/11): `memory` (`"256mb"`, byte một câu được dùng: kết quả trả về cộng file
  tạm của SQLite; vắng: mặc định của KitDB) và `timeout` (`"500ms"`, `"30s"`, `"2m"`, đọc bằng `value.TimeSpan`, như `.timeout` của http, queue,
  lịch; như `statement_timeout` của PostgreSQL; vắng: 30 giây). `memory` giữ nghĩa của website, trần byte của một lượt
  chạy, chỉ khác lượt chạy là một câu SQL. Không có `energy`: cổng chạy SQL trên SQLite, không chạy JS trên VM. Không
  có cổng thì hai trần không dùng (engine in một dòng). Trong KitDB hôm nay kết quả trả về và file tạm là hai trần
  riêng (`MaxBytes`, `MaxTemporaryBytes`): engine phải nối `memory` vào cả hai, hay KitDB gộp chúng (chưa làm).
  Object đi qua `keys` viết tay (mỗi chữ một `case`, đúng tên chữ; khoá lạ là lỗi nêu các tên đầy đủ). `value.Assign`
  (dò bằng reflect) vẫn ở `core/value`.
- Tên thứ hai (owner 23/11) còn một loại: **tên thứ hai của một cài đặt cùng nghĩa** (`port` của `listen`, `certs` của
  `certificates`). `port` có ở mọi cửa có `listen`: của website nó là chữ cuối thứ hai (`.port(8080)` như
  `.listen(8080)`), của database nó là khoá lẫn chữ, cùng một trường, nên `{ listen: 443, port: 80 }`
  giữ cả hai. Test từ vựng kê chữ thứ hai trong `aliases` (`Port` ghi trường `Listen`). Lời báo khoá lạ chỉ nêu tên đầy
  đủ. Tên tắt của một cửa (`web`, `cron`, `db`, `cli`, owner 23/11) **bỏ** (owner 24/11): chúng có để `.run({ web, db })`
  viết được bằng tên biến, mà không còn gì truyền bằng tên biến; viết chúng làm khoá là lỗi nói câu của cửa.
- `run`, `check` của Application là động từ, không phải giá trị: dấu `bool` (đọc bằng `Running()`, `Checking()`), không
  đối số (`app.run({ web })` là lỗi nói câu của cửa), một trong hai, và là dòng cuối: `writable` (helper.go) từ chối mọi
  chữ viết sau nó. `application({ run: … })` là lỗi: run là động từ, viết ở cuối.
- `listen` là của cửa nghe nó: chữ cuối của website, khoá `listen`/`port` của database. Application không có cổng của
  riêng nó (owner 24/11; cổng ở mức chương trình của 23/11, thêm vào mọi cửa, gỡ hẳn). Các chữ chỉ thêm, không lọc: bản
  thiết kế giữ đúng những gì người viết viết. Engine lọc khi nạp (nên `app.check()` cũng thấy), không lỗi, in ra mỗi
  địa chỉ bỏ. Code và test: `host/listen.go` (`Addresses`, `Filter`, `Sockets`, `Use.Accepts`), `test/host/`.
  - địa chỉ là một IP và một cổng. Host chưa chỉ định (`":443"`, `"0.0.0.0:443"`, `"[::]:443"`) là một: mọi giao
    diện, IPv4 và IPv6 (với `"tcp"`, Go mở cả ba thành một socket, `net.favoriteAddrFamily`). `localhost` là
    `127.0.0.1` và `::1`, cố định (RFC 6761), không hỏi DNS; tên máy khác là lỗi (owner 23/11), để boot không phụ thuộc
    DNS và `.check` ra như nhau mọi máy. Máy không có IPv6 thì engine bỏ `::1` khi mở socket (chưa làm);
  - lớn chứa nhỏ: `":443"` chứa mọi IP của cổng 443 (`"127.0.0.1:443"`, `"[::1]:443"`);
  - trong một cửa: trùng hay bị chứa thì bỏ (không mất gì: cửa đã có địa chỉ lớn);
  - giữa các cửa: chỉ lọc socket (engine mở hợp địa chỉ mọi cửa, lớn chứa nhỏ); mỗi cửa giữ địa chỉ của nó. Bộ chia đọc
    byte đầu (SSLRequest, StartupMessage của PostgreSQL thì database; TLS thì sau bắt tay, ALPN `postgresql` thì
    database; còn lại website), rồi đưa cho cửa ấy chỉ khi địa chỉ kết nối đi vào (`LocalAddr`) thuộc địa chỉ của nó
    (`Use.Accepts`, so theo IP: trên socket mọi giao diện, kết nối IPv4 đến với `::ffff:127.0.0.1`):
    `app.database({ listen: "127.0.0.1:443" })` cùng `app.website().listen(443)` chung socket `:443` mà database không lộ
    ra ngoài.

  Bộ chia chưa làm; KitDB còn thiếu nhận TLS thẳng (PostgreSQL 17 `sslnegotiation=direct`). Đề xuất "listen mà không
  cửa nào nghe là lỗi" không còn chỗ đứng: không còn listen nào đứng ngoài một cửa.
- `Fields()`, `Share()`, `Shared()` không phải chữ JS (kết quả không phải `value.Value`). `Share()` là `value.Sharer`:
  engine đóng băng bản thiết kế sau khi nạp, nó đánh dấu chỉ đọc và trả mọi giá trị nó giữ (`value.Held(w.fields)`,
  không liệt kê tay). Từ đó mọi chữ ghi lên nó là lỗi (`… was loaded by the engine and is read only now`): chỗ từ chối là
  `set`/`add` và chữ cuối của cửa, và `writable` của Application (trước `set`, `add`, `open`, `verb`), vì mọi phép ghi
  đi qua chúng. Luật 1 đã không cho một guard giữ `app` hay một cửa; đây là lớp thứ hai, cho Go gọi thẳng.
- Mỗi cửa có một test từ vựng (`test/blueprint/vocabulary_test.go`): mỗi chữ ghi đúng một trường khớp tên, dạng
  object (`{ guard: x }`) ghi đúng trường của chữ, lời báo khoá lạ nêu đúng các khoá, chữ danh sách thêm vào cuối, mọi
  trường có chữ, sau Share mọi chữ là lỗi, chữ thứ hai (`aliases`) ghi trường của chữ đầu; mỗi chữ cuối (`closing`) ghi
  đúng trường của nó, đóng câu, trả về app và không phải khoá; Application thêm: hai động từ, mỗi cái đặt đúng dấu của
  nó, và bốn chữ cửa, mỗi cái dựng đúng loại cửa vào đúng trường cùng tên.
- Lỗi là `value.Value` loại invalid (code, message, at): `response.error`, `job.error`, `command.error`. Không có struct
  Failure riêng như docs/host. Đã có trong `core/value/failure.go` (23/11): `value.Fail(code, format, …)` là lỗi có mã;
  `v.Within(path)` gắn chỗ, đặt trước chỗ đã có (lỗi ở `timeout` Within `application.database` là ở
  `application.database.timeout`; `[1]` dính vào tên trước nó), nên mỗi tầng chỉ nói tên của mình; đọc bằng `Code()`,
  `Message()` (không có chỗ), `At()`; `Error()` vẫn là error của Go (`"at: message"`), errors.Is thấy lỗi gốc. Lỗi của
  `Invalid`, `Error` không mã, gắn chỗ được. Chưa có: runtime đưa lỗi thành object `{ code, message, at }` cho JS
  (`safe(…)`), và bộ nạp thật trả lỗi bằng `Within` thay chuỗi ghép tay của demo.
- Desktop và mobile dùng `website.RequestContext` cho memory, energy và các hàm sau khi gửi; không có ssl, guards, warm.

## Đề xuất mới trong lần này (chưa duyệt)

- `command.Invocation` thay tên `Run` của docs/host: `Run` để dành cho lượt chạy của runtime.

## Còn mở

1. JS gọi tới Go: `web.guard(fn)` tới phương thức nào, theo luật nào (luật chữ ký của engine2, hay chữ ký có `run`).
2. ~~Trường không xuất ra thì code nào đọc chúng~~: `Fields()` (Application, Website, Database, Schedule, Command;
   desktop, mobile theo khi làm).
3. `success`/`errors`/`finally`: hai số ít, một số nhiều; JS viết `.error(fn)`.
4. Mã lỗi của guard từ chối (`"REFUSED"`?) và tập mã chung (cách mang mã đã có: `value.Fail`; tập mã chưa chốt).
5. Thư mục của schedule (giá trị của chữ cuối `run`): thư mục `.schedules/` hay gốc app (code tự khai từ 17/11).
6. Các chữ chưa có chỗ: `app.root`, `app.hotReload`, `app.databases`, `app.postgresql`/`.connect`,
   `app.shareEnvironment`, `app.history`.

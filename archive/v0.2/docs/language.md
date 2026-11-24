# Đặc tả ngôn ngữ `.work`

Tài liệu này là đặc tả của ngôn ngữ viết các file `.work` trong engine2: nó tả đúng những gì `core/compiler`,
`core/runtime`, `core/value` và `core/builtins` nhận và làm hôm nay (18/11/2026). Đổi ngôn ngữ thì sửa tài liệu này trong
cùng commit với code. Vì sao từng quyết định được chọn, và lịch sử của chúng, nằm ở `docs/KITWORK_LANG.md` của repo gốc.

**`.work` là JavaScript, chạy trên máy của Kitwork.** Cú pháp và nghĩa là của JavaScript (một ES module, luôn strict),
theo luật chốt ngày 27/10/2026: **mỗi cách viết hoặc có đúng nghĩa của JavaScript, hoặc là lỗi biên dịch; không bao giờ
ra một giá trị khác.** Người và AI đã biết JavaScript đọc và viết được ngay; thứ Kitwork thêm nằm ở máy chạy, không ở cú
pháp:
- **tập con có chủ đích:** không `while`, `try`/`throw`, `class`, `this`, `async`/`await`, prototype (mục 3);
- **lỗi là `return error(…)` và `safe(() => …)`** (mục 5);
- **quyền theo import:** mạng, file, database chỉ có qua `import … from "kitwork/<tên>"`, và mỗi thư viện chỉ ở những
  file được phép (mục 6);
- **mỗi lần chạy có trần:** năng lượng, byte, khung gọi (mục 7);
- **dữ liệu cấp file đóng băng sau boot**, vì mọi request cùng đọc nó (mục 1.7);
- **ba loại giá trị riêng:** thời gian, khoảng thời gian, bytes (mục 1.1).

**Chọn giữa nghĩa của JavaScript và lỗi** (chủ dự án chốt 19/11/2026). `.work` không chép hết JavaScript và không mở
rộng cú pháp của nó. Khi một cách viết của JavaScript chưa theo luật trên, nó được chọn theo một câu hỏi: người ta có viết
nó thật không. Cách viết người ta dùng thật (`Array(n).fill(0)`, `Object.groupBy`, chú thích `/* … */`) có đúng nghĩa của
JavaScript. Cách viết hiếm hay tối nghĩa (`String(Math)`, `f.caller`, `Object(5)`, cách V8 làm tròn `parseInt` ở cơ số 36)
là lỗi nói cách viết thay, không chép hành vi lạ của JavaScript vào engine. Người viết không bao giờ nhận một giá trị khác
JavaScript; engine không mang thứ không ai dùng.

Chỗ engine hôm nay chưa giữ luật trên ghi ở mục 8; quyết định đã chốt mà engine chưa làm ở mục 9. Biểu thức trong file
`.ui` theo cùng nghĩa, cộng hai luật của markup: mọi truy cập mang nghĩa `?.`; `null`, `undefined` in ra rỗng
(`docs/KITWORK_LANG.md` mục 3 của repo gốc, 31/10/2026).

## Mục lục

1. [Giá trị và kiểu](#1-giá-trị-và-kiểu-corevalue)
2. [Khai báo và phạm vi](#2-khai-báo-và-phạm-vi)
3. [Cú pháp](#3-cú-pháp): ký hiệu, từ vựng, biểu thức, câu lệnh, hàm, mẫu tách, kiểm `null`
4. [Thư viện sẵn có](#4-thư-viện-sẵn-có-corebuiltins)
5. [Mô hình xử lý lỗi: `error` và `safe`](#5-mô-hình-xử-lý-lỗi-error-và-safe)
6. [Chương trình, file và module](#6-chương-trình-file-và-module)
7. [Giới hạn của một lần chạy](#7-giới-hạn-của-một-lần-chạy)
8. [Chỗ engine chưa giữ luật](#8-chỗ-engine-chưa-giữ-luật)
9. [Đã chốt, engine chưa làm](#9-đã-chốt-engine-chưa-làm)
10. [Ghi chú hiện thực](#10-ghi-chú-hiện-thực)

## 1. Giá trị và kiểu (`core/value`)

`.work` có kiểu động như JavaScript: giá trị mang loại của nó, tên thì không. Trong VM mỗi giá trị là một `value.Value`
cố định 32 byte; số giữ tại chỗ nên phép tính không cấp phát (`core/value/value.go`). Loại của giá trị không ra khỏi gói;
chương trình hỏi `typeof`.

### 1.1. Các loại

| Loại | `typeof` | Tạo bằng | Ghi chú |
|---|---|---|---|
| undefined | `"undefined"` | `undefined`, khoá vắng, hàm không `return` | |
| null | `"object"` | `null` | |
| boolean | `"boolean"` | `true`, `false` | |
| number | `"number"` | chữ số, `NaN`, `Infinity` | số thực 64 bit IEEE-754, có `-0` |
| string | `"string"` | chuỗi, chuỗi mẫu | bất biến; độ dài, chỉ số, thứ tự theo đơn vị UTF-16 |
| array | `"object"` | `[a, b]` | liền, không lỗ; chỉ nhận khoá là chỉ số |
| object | `"object"` | `{ a: 1 }` | chỉ có khoá riêng, không chuỗi prototype |
| function | `"function"` | `function`, `=>`, builtin, method đọc không gọi | |
| thời gian | `"object"` | `new Date(…)` | một mốc tới nano giây, trong khoảng 1678–2262; của Kitwork thay object `Date` (mục 4.3) |
| khoảng thời gian | `"object"` | thời gian − thời gian; từ module | của Kitwork; luôn truthy |
| bytes | `"object"` | từ module (đọc file, body của request) | của Kitwork; `length`, `[i]` |
| object của module | `"object"` | module (`context`, `request`, bảng database…) | method của nó, tả ở tài liệu module |

Lỗi (mục 5) không phải một giá trị chương trình cầm được: nó dừng lời gọi ngay ở chỗ sinh ra.

### 1.2. Chuỗi

Chuỗi lưu bằng UTF-8 nhưng mang nghĩa của JavaScript: `length` đếm đơn vị UTF-16 (`"😀".length` là 2), `s[i]` và
`charCodeAt` đọc một đơn vị (`"a😀b"[1]` là nửa đầu surrogate), chỉ số ngoài khoảng là `undefined`, `<` so theo đơn vị
UTF-16. `for…of` và `...` đi theo code point. Nửa surrogate đứng riêng được giữ, ghép lại thì thành ký tự.

### 1.3. Số

Số là IEEE-754 64 bit: `0.1 + 0.2` là `0.30000000000000004`, `1 / 0` là `Infinity`, `NaN !== NaN`,
`Object.is(-0, 0)` là `false`, `%` giữ dấu của số bị chia. Chữ của số theo `Number.prototype.toString` của JavaScript
(`"1e+21"`, `"1e-7"`, `String(-0)` là `"0"`).

### 1.4. Đổi kiểu

Theo JavaScript:
- **Thành số:** chuỗi bỏ khoảng trắng hai đầu rồi đọc như chữ số (`" 12 "` là 12, `"0x10"` là 16, `""` là 0, `"12px"`,
  `"1_000"` là `NaN`); `null` là 0, `undefined` là `NaN`, `true` là 1; mảng qua chữ của nó (`[5]` là 5, `[1, 2]` là
  `NaN`); object qua `valueOf` rồi `toString`; thời gian là mili giây; khoảng thời gian là mili giây.
- **Thành chuỗi:** `null` là `"null"`, mảng nối bằng dấu phẩy (`null`, `undefined` thành rỗng), object là
  `"[object Object]"`, khoảng thời gian là dạng của Go (`"1h0m0s"`). Một hàm không có chữ:
  JavaScript viết mã nguồn của nó, Kitwork không giữ, nên đổi một hàm thành chuỗi (`String(f)`, `` `${f}` ``, `f + ""`,
  `f.toString()`, `[f].join()`, chuỗi mà một builtin đọc từ đối số, `parseInt(f)`, `"…".includes(f)`, khoá `o[f]`,
  `f == "x"`) là lỗi `TYPE` (`một hàm không đổi thành chữ: …`). Hàm thay của `replace`, `replaceAll` vẫn được gọi lại;
  `console.log(f)` viết `[Function: f]` như node; `{{ f }}` của trang `.ui` là lỗi vẽ trang. Một thời gian cũng không có
  chữ: JavaScript viết nó theo múi giờ của máy chạy, nên đổi một thời gian thành chuỗi (cùng các chỗ ấy, và `t + 1`,
  `t == 0`, sắp xếp mảng có thời gian không có hàm so) là lỗi `TYPE` (`một thời gian không đổi thành chữ: …`); viết rõ
  dạng, `t.toISOString()` hay `t.format(…)` (mục 4.3). `console.log(t)` viết `toISOString()` như node; `{{ t }}` của
  trang `.ui`, và một thời gian làm khoá hàng (`key:`), là lỗi vẽ trang.
- **Thật giả:** chỉ `false`, `0`, `-0`, `NaN`, `""`, `null`, `undefined` là falsy; `"0"`, `[]`, `{}`, mọi hàm, mọi thời
  gian và khoảng thời gian (kể cả khoảng 0) là truthy.
- **Thành giá trị nguyên thuỷ:** chỉ `valueOf`, `toString` riêng của object được gọi (không có prototype), không có
  `this`. Gợi ý số và mặc định: `valueOf` trước; gợi ý chuỗi (chuỗi mẫu, `String(…)`): `toString` trước. Không cái nào ra
  giá trị nguyên thuỷ là lỗi `TYPE` (`cannot convert object to primitive value`). Mảng ra chữ của nó.

### 1.5. Object và mảng

- **Thứ tự khoá:** khoá là chỉ số mảng (số nguyên từ 0 tới 2^32 − 2, viết chuẩn) trước, tăng dần; rồi các khoá khác
  theo thứ tự thêm vào, như JavaScript. Gán lại một khoá giữ chỗ cũ của nó.
- Khoá đổi thành chuỗi (`o[1]` là `o["1"]`, `o[{}]` là `o["[object Object]"]`).
- Không có chuỗi prototype: viết thẳng `constructor` hay `__proto__` (`o.constructor`, `o["__proto__"]`, khoá `__proto__: p`
  của một object, `const { constructor } = o`) là lỗi biên dịch (`Kitwork không có prototype nên không có constructor:
  hỏi loại của một giá trị thì typeof x, Array.isArray(x)`); `{ __proto__ }` viết tắt và khoá tính `[k]` là khoá thường,
  như JavaScript; khoá đọc bằng biến (`o[k]`) đọc khoá riêng. Ba method chung `hasOwnProperty`,
  `toString`, `valueOf` có trên mọi giá trị trừ `null`, `undefined`, và một khoá riêng cùng tên che chúng.
- **Ghi:** ghi qua `null`, `undefined`, số, boolean, chuỗi, hàm hay object của module là lỗi `TYPE` (`không ghi được
  s.foo: s là string, không phải object hay mảng`). Ghi vào thời gian, khoảng thời gian, bytes cũng là lỗi `TYPE`
  (`không ghi được t.x: t là time, không phải object hay mảng`): chúng không có chỗ cho khoá (mục 8.4).
- **Mảng:** `a[i] = v` quá cuối đệm `undefined` (khoảng đệm tối đa 1 048 576 phần tử, quá là `INVALID_ARGUMENT`); khoá
  không phải chỉ số (`a["x"]`, `a[-1]`, `a[1.5]`) và gán `a.length` là lỗi `UNSUPPORTED`. Đọc khoá như vậy ra
  `undefined`; `a["1"]` đọc phần tử 1.

### 1.6. Bằng nhau

- `===`: object, mảng, hàm bằng nhau khi là cùng một giá trị; `NaN` không bằng gì. Thời gian, khoảng thời gian, bytes bằng
  nhau theo giá trị (mục 8). Cùng một method đọc hai lần thì bằng nhau (`[1].map === [2].map`).
- `==` theo JavaScript: `null == undefined`, không bằng thứ khác; số với chuỗi, boolean so theo số; object với giá trị
  nguyên thuỷ đổi object trước; hai object khác nhau không bao giờ bằng.
- `Object.is` như JavaScript (`Object.is(NaN, NaN)` là `true`, `Object.is(-0, 0)` là `false`).

### 1.7. Đóng băng sau khi boot

Phần cấp cao nhất của mỗi file chạy một lần lúc boot. Xong, mọi object, mảng và hàm với tới được từ tên cấp file bị đóng
băng sâu (`core/runtime/freeze.go`), vì mọi request, của mọi người dùng, cùng đọc chúng:
- ghi vào chúng lúc request là lỗi `TYPE` (`cannot add property 2, object is not extensible`, `cannot assign to read
  only property 'a' of object '#<Object>'`); gán lại một `let` cấp file cũng vậy (`cannot assign to count: it belongs to
  the code that ran as the site booted, which every request shares and only reads`);
- `toString`, `valueOf` riêng của một object dựng lúc boot không chạy được trong một phép đổi kiểu lúc request (lỗi
  `TYPE`); gọi thẳng method của nó thì được;
- bản chép (`[...list]`, `{ ...obj }`) ghi được;
- `Object.isFrozen` của chúng là lỗi `TYPE` (19/11/2026; trước đó `true`, JavaScript `false` vì JavaScript để chúng mở),
  cũng như của mọi builtin; thứ chính file đóng băng bằng `Object.freeze` là của chương trình, `Object.isFrozen` là `true`.

## 2. Khai báo và phạm vi

```ebnf
Declaration    = ( "const" | "let" | "var" ) Binding .      /* mỗi câu một tên hay một mẫu */
Binding        = identifier [ "=" Expression ]               /* const bắt buộc có "=" */
               | Pattern "=" Expression .
FunctionDecl   = "function" identifier Parameters Block .
```

### 2.1. `const`, `let`, `var`: mỗi chỗ một cách viết

| Chỗ | Viết | Lỗi biên dịch |
|---|---|---|
| Cấp cao nhất của file (cả file được import) | `const`, `function`, `import`, `export` | `var`, `let` (`let x ở cấp cao nhất của file: tên đổi được ở cấp file sống qua mọi request, của mọi người dùng; ở đây viết const`) |
| Cấp cao nhất của hàm `router.view(…)` hay `component.define(…)` | `var` (trạng thái của màn hình), `const` (dữ liệu, hàm) | `let` (`let a ở cấp cao nhất của hàm router.view(…) hay component.define(…): trạng thái của màn hình viết var, giá trị không đổi viết const`) |
| Mọi chỗ khác: thân hàm, khối, vòng lặp | `let`, `const` | `var` (`var t chỉ đứng ở cấp cao nhất của hàm router.view(…) hay component.define(…), là trạng thái của màn hình; ở đây viết let, hay const nếu không đổi`) |

- `const` cần giá trị ngay (`const cần giá trị ngay khi khai báo, như JavaScript: const x = …; chưa có giá trị thì
  let x;`); `let x;` là `undefined`.
- Mỗi câu khai một tên hay một mẫu: `let a = 1, b = 2` là lỗi biên dịch, viết hai dòng.
- **Hàm view.** Compiler nhận ra hàm của `X.view(fn)` (một đối số), `X.define(tên, fn)` (hai đối số) và chuỗi bắt đầu từ
  `X` như `router.guard(g).view(fn)`, viết là một câu lệnh ở cấp cao nhất của file, với `X` là tên của
  `import * as X from "kitwork/router"` hay `"kitwork/component"`; `fn` viết tại chỗ hay là tên của một `const` cấp
  file giữ hàm. `var` ở đó cần hàm có tham số `context`, mỗi dòng một tên (`var query = ""; var page = 1;`). Nghĩa của
  trạng thái màn hình ở [`view.md`](view.md), [`component.md`](component.md).
- Luật của `let` theo chỗ áp cho file (`CompileFile`); một đoạn mã biên dịch riêng (`CompileSource`, của test) chạy cấp cao
  nhất của nó một lần, nên nhận `let` ở đó.

### 2.2. Phạm vi

- `const`, `let` sống trong khối khai nó, như JavaScript. Khối là thân hàm, thân `if`/`else`, đầu và thân một vòng `for`
  (đầu là một khối, thân là khối con), thân một `switch` (mọi `case` chung một khối), một khối `{ … }` đứng riêng.
- **Tên phải được khai.** Đọc hay gán một tên mà không gì khai (`const`, `let`, `var`, `function`, tham số, `import`) và
  không phải builtin (mục 4) là lỗi biên dịch ở chỗ tên ấy, như JavaScript ném `ReferenceError`:
  `y chưa được khai: khai nó bằng const hay let, hay import nó; JavaScript ném ReferenceError ở đây`,
  `y = … gán vào một tên chưa khai: khai nó ở chỗ nó bắt đầu, let y = …; JavaScript (strict) ném ReferenceError ở đây`.
  Gán vào tên builtin (`Math = 5`) cũng là lỗi này. `typeof y` của một tên trần chưa khai là `"undefined"`; trong một
  chuỗi (`typeof y?.z`) thì vẫn là lỗi.
- **Dùng trước khi có** (`let`, `const`), như "vùng chết tạm thời" của JavaScript (mục 9 chốt 27/10/2026, làm 18/11/2026):
  - trong cùng hàm, dùng trước dòng khai là lỗi biên dịch: `x được dùng trước dòng khai nó (f:1:25): JavaScript ném
    ReferenceError ở đây; dời dòng khai lên trước, hoặc dùng nó trong một hàm được gọi sau dòng ấy`;
  - đọc trong chính dòng khai (`let x = x`, `const x = x + 1`) là lỗi biên dịch; một hàm tạo trong dòng ấy thì đọc được
    (`const f = () => f`);
  - một `case` sau dùng `let`, `const` của một `case` trước, trực tiếp hay qua một hàm tạo ở đó, là lỗi biên dịch: case
    khai nó không chạy thì JavaScript ném `ReferenceError`; khai trước `switch`, hay bọc mỗi `case` trong `{ }`;
  - một hàm tạo trước dòng khai (một `function` được đưa lên đầu khối, hay một hàm viết trước dòng ấy) chạy trước khi
    dòng khai chạy thì đọc hay ghi biến ấy là lỗi lúc chạy mã `NOT_INITIALIZED` ở chỗ dùng (`x chưa có: một hàm dùng nó
    trước khi dòng khai nó chạy; …`), `safe` bắt được. Phạm vi của biến đánh dấu nó lúc bắt đầu, chỉ khi có một hàm như
    vậy; biến khác không tốn gì;
  - biến của một lượt vòng lặp mà một hàm tạo trước dòng khai nó dùng là lỗi biên dịch (lượt vòng lặp chưa có dấu ấy):
    dời dòng khai lên trước hàm.
- **`function` được đưa lên đầu khối của nó:** gọi trước dòng khai được, như JavaScript. Hàm khai trong một `case` có từ
  đầu khối của `switch`, trước biểu thức của `case` đầu tiên. Tên của một `function` gán lại được, như JavaScript.
- **Khai lại** một tên trong cùng khối là lỗi biên dịch nêu chỗ khai trước (kể cả `function` với `function`, `import`
  với `const`, hai tên trong một mẫu); trùng tên tham số cũng vậy (`a đã là tham số của hàm …`). Khối con, hàm con khai
  lại được.
- **Gán vào hằng** (`const`, tên import, biến của `for (const …)`) bằng `=`, `++`, `+=` là lỗi biên dịch nêu chỗ khai:
  `x là hằng (const), khai báo ở f:1:7, không gán lại được, như JavaScript: khai báo bằng let nếu cần đổi giá trị`; biến
  của `for (const … of …)` và biến đếm khai bằng `const` có thông báo riêng của chúng.
- **Che builtin** bằng một khai báo được (`const Math = 5`). `undefined` là từ khoá, không khai được. `kitwork` là tên của
  chính Kitwork: khai, gán hay làm tham số là lỗi biên dịch.
- **Hàm tạo trong vòng lặp** giữ biến của lượt nó, như JavaScript: `for (let i = 0; i < 3; i++) { fs.push(() => i); }`
  cho 0, 1, 2; `for (const x of list)` cũng vậy; `let x;` trong thân là `undefined` mỗi lượt. Hàm giữ biến theo tham chiếu.
- **Tầm với của hàm lồng nhau.** Một hàm đọc được biến của tối đa 255 tầng hàm bao ngoài (lỗi biên dịch khi quá).

## 3. Cú pháp

### 3.1. Cách viết văn phạm

Văn phạm viết theo EBNF như spec của Go: `Tên = vế phải .`; `|` là hoặc, `( … )` là nhóm, `[ … ]` là có hay không,
`{ … }` là lặp không hay nhiều lần; chữ trong ngoặc kép là chính chữ ấy; `/* … */` trong văn phạm là ghi chú.
Tên viết thường (`identifier`, `string_lit`) là token của mục 3.2; tên viết hoa đầu là một phần của văn phạm.

```ebnf
SourceFile     = { Statement } .
```

### 3.2. Từ vựng

**Văn bản nguồn** là UTF-8 và được đọc theo byte. Ngoài chuỗi, chuỗi mẫu và chú thích, chỉ có ASCII; thứ khác là lỗi
biên dịch nêu đúng thứ gặp: BOM ở đầu file (`file bắt đầu bằng BOM (U+FEFF): lưu file là UTF-8 không có BOM`), khoảng
trắng Unicode (NBSP), U+2028, U+2029, chữ có dấu trong tên (`ký tự 'é' ngoài chuỗi và chú thích: …`), byte không phải
UTF-8. Trong chuỗi và chú thích, byte nào cũng giữ nguyên.

```ebnf
whitespace     = " " | /* tab */ | /* VT */ | /* FF */ | newline .
newline        = /* LF */ | /* CR LF */ .
comment        = line_comment | block_comment .
line_comment   = "//" { /* mọi byte trừ LF, CR, U+2028, U+2029 */ } .
block_comment  = "/*" { /* mọi byte trừ CR đứng riêng, U+2028, U+2029; không gồm "*/" */ } "*/" .
```

- **Dòng mới** là LF hay CRLF. CR đứng riêng (dòng mới kiểu Mac cũ), byte NUL và byte điều khiển khác ngoài chuỗi là lỗi
  biên dịch nêu byte ấy.
- **Chú thích** là `//`, tới dòng mới (LF, CR, U+2028, U+2029, như JavaScript), và `/* … */` (chủ dự án mở 19/11/2026),
  tới `*/` đầu tiên sau `/*` của nó: `/*/` không đóng nó, chú thích không lồng nhau, `/*` trong chuỗi là chữ. Một chú thích
  `/* … */` có dòng mới bên trong là một dòng mới giữa hai token quanh nó, như JavaScript: hai câu lệnh nó chia không cần
  `;`, `return /* … dòng mới … */ 1` trả `undefined`. Bên trong nó có CR đứng riêng, U+2028 hay U+2029 là lỗi biên dịch
  như ở ngoài, để dòng được đếm như nhau ở mọi chỗ; `/*` không có `*/` tới hết file là lỗi biên dịch tại `/*` (`chú thích /*
  chưa đóng: file hết trước */`). Không có `#!` ở đầu file hay `<!--`: lỗi biên dịch.

**Tên.**

```ebnf
letter         = "A" … "Z" | "a" … "z" | "_" | "$" .
decimal_digit  = "0" … "9" .
identifier     = letter { letter | decimal_digit } .
```

Tên chỉ là ASCII (`const café` là lỗi); không có `\u` trong tên.

**Từ khoá.** Các chữ sau không làm tên được (`X là từ khoá của ngôn ngữ, không làm tên biến được; làm khoá object thì
viết thẳng { X: … } hoặc { "X": … }, đọc thì o.X`); làm khoá object và tên thành viên thì được (`o.if`, `{ default: 1 }`):

```
break     case      catch     class     const     continue  default   delete    do        else
export    false     finally   for       function  if        import    in        instanceof
let       new       null      return    switch    throw     true      try       typeof    undefined
var       void      while
```

`class`, `while`, `do`, `try`, `catch`, `finally`, `throw`, `in`, `instanceof` là từ khoá chỉ để báo lỗi kèm
cách viết thay (mục 3.3, 3.4). `undefined` là một chữ như `null`, không phải tên. `kitwork` không phải từ khoá nhưng không
khai, không gán được (mục 2.2). `of`, `as`, `from`, `get`, `set` là tên thường. `this`, `super`, `arguments`, `yield`,
`async`, `await`, `eval`, `with`, `debugger` là tên thường: không khai thì dùng chúng là lỗi tên chưa khai (mục 2.2).
Những tên mà một module JavaScript (strict) giữ riêng không khai được (`this`, `super`, `yield`, `await`, `enum`,
`static`, `implements`, `interface`, `package`, `private`, `protected`, `public`, `eval`, `arguments`): `const this = 1`
là lỗi biên dịch.

**Dấu.**

```
===   !==   ==    !=    =>    =     !     ++    +=    +     --    -=    -     *=    *
/=    /     %     <=    <     >=    >     &&    ||    ??    ?.    ?     :     ;
,     ...   .     (     )     [     ]     {     }
```

`?.` theo sau là một chữ số thì là `?` rồi một số (`x?.5 : 2` là toán tử ba ngôi), như JavaScript. Không có `**`, `&`,
`|`, `^`, `~`, `<<`, `>>`, `>>>`, `%=`, `**=`, `&&=`, `||=`, `??=`, `#`, `@`; `&`, `|`, `^`, `~` báo `Kitwork không có
toán tử bit`.

**Số.**

```ebnf
number_lit     = decimal_lit | hex_lit | octal_lit | binary_lit .
decimal_lit    = decimal_int [ "." [ decimal_digits ] ] [ exponent ]
               | "." decimal_digits [ exponent ] .
decimal_int    = "0" | ( "1" … "9" ) [ [ "_" ] decimal_digits ] .
decimal_digits = decimal_digit { [ "_" ] decimal_digit } .
exponent       = ( "e" | "E" ) [ "+" | "-" ] decimal_digits .
hex_lit        = "0" ( "x" | "X" ) hex_digit { [ "_" ] hex_digit } .
octal_lit      = "0" ( "o" | "O" ) octal_digit { [ "_" ] octal_digit } .
binary_lit     = "0" ( "b" | "B" ) binary_digit { [ "_" ] binary_digit } .
```

- Mọi số là số thực 64 bit; chữ số quá độ chính xác làm tròn về số gần nhất, `1e400` là `Infinity`.
- `_` chỉ đứng giữa hai chữ số (`1_000`); chỗ khác là lỗi (`dấu _ trong một số chỉ đứng giữa hai chữ số, như 1_000`).
- 0 đứng đầu rồi một chữ số (`010`, `08`) là lỗi, như JavaScript strict: viết `0o10` hay bỏ số 0.
- Ngay sau một số không được là chữ hay chữ số (`3in`, `0b12`, `1.toString()`): lỗi. `1..toString()` và
  `1.5.toFixed(1)` được.
- `1n` là lỗi: không có BigInt.

**Chuỗi.**

```ebnf
string_lit     = `"` { char | escape | line_cont } `"` | "'" { char | escape | line_cont } "'" .
escape         = "\" ( "b" | "f" | "n" | "r" | "t" | "v" | "0"
                     | "x" hex_digit hex_digit
                     | "u" hex_digit hex_digit hex_digit hex_digit
                     | "u{" hex_digit { hex_digit } "}"          /* tới 10FFFF */
                     | non_escape ) .
line_cont      = "\" newline .
```

- `char` là mọi byte trừ dấu đóng, `\`, LF và CR; chuỗi chưa đóng trước khi hết dòng là lỗi (`chuỗi chưa đóng: thiếu "
  trước khi hết dòng; chuỗi nhiều dòng thì viết template`).
- `\0` không được theo sau bởi một chữ số. Escape bát phân (`\1`, `\08`) là lỗi, như JavaScript strict. `\q` là `q`.
- `\x`, `\u` sai dạng là lỗi nêu dạng đúng.
- Hai nửa surrogate liền nhau ghép thành một ký tự (`"\u{D83D}\uDE00" === "😀"`); nửa đứng riêng được giữ.

**Chuỗi mẫu.**

```ebnf
template_lit   = "`" { template_char | escape | line_cont | "${" Expression "}" } "`" .
```

`$` không theo sau bởi `{` là chữ; dòng mới giữ nguyên (CRLF thành LF); `${ }` lồng được, chuỗi mẫu trong `${ }` cũng vậy.
`${ }` rỗng hay chứa hai biểu thức là lỗi. Không có chuỗi mẫu có thẻ: một chuỗi mẫu ngay sau một biểu thức, cùng dòng
hay dòng sau, là lỗi biên dịch (`Kitwork chưa có chuỗi mẫu có thẻ (tag`…`): gọi hàm với chuỗi mẫu làm đối số,
tag(`…`); …`), vì JavaScript đọc nó là một lời gọi.

**Không có:** biểu thức chính quy (`/ab+c/`), BigInt, tên riêng `#x`, decorator `@x`.

### 3.3. Biểu thức

```ebnf
Expression     = UnaryExpr
               | Expression binary_op Expression
               | Expression "?" Expression ":" Expression
               | Target assign_op Expression
               | ArrowFunction .
binary_op      = "||" | "??" | "&&" | "==" | "!=" | "===" | "!==" | "<" | "<=" | ">" | ">="
               | "+" | "-" | "*" | "/" | "%" .
assign_op      = "=" | "+=" | "-=" | "*=" | "/=" .
UnaryExpr      = PostfixExpr | unary_op UnaryExpr | ( "++" | "--" ) UnaryExpr | NewExpr .
NewExpr        = "new" ( "Date" | "Array" | "Object" ) [ Arguments ] .
unary_op       = "!" | "-" | "+" | "typeof" | "void" | "delete" .   /* delete: chỉ trước o.k, o[k] */
PostfixExpr    = PrimaryExpr [ "++" | "--" ] .              /* không xuống dòng trước ++, -- */
PrimaryExpr    = Operand
               | PrimaryExpr "." IdentifierName
               | PrimaryExpr "[" Expression "]"
               | PrimaryExpr Arguments
               | PrimaryExpr "?." ( IdentifierName | "[" Expression "]" | Arguments ) .
Arguments      = "(" [ ListItem { "," ListItem } ] ")" .    /* không dấu phẩy cuối, tối đa 255 mục */
ListItem       = [ "..." ] Expression .
Operand        = Literal | identifier | TemplateLiteral | ArrayLiteral | ObjectLiteral
               | FunctionExpression | "(" Expression ")" .
Literal        = number_lit | string_lit | "true" | "false" | "null" | "undefined" .
ArrayLiteral   = "[" [ ListItem { "," ListItem } ] "]" .    /* không lỗ trống, không dấu phẩy cuối */
ObjectLiteral  = "{" [ Property { "," Property } [ "," ] ] "}" .
Property       = "..." Expression | PropertyName ":" Expression | identifier .
PropertyName   = IdentifierName | string_lit | number_lit | "[" Expression "]" .
Target         = identifier | PrimaryExpr "." IdentifierName | PrimaryExpr "[" Expression "]" | Pattern .
```

`IdentifierName` sau dấu chấm nhận cả từ khoá (`o.function`, `o.if`, `{ default: 1 }`), như JavaScript.

**Thứ tự ưu tiên**, từ chặt tới lỏng:

| Mức | Toán tử | Kết hợp |
|---|---|---|
| 1 | `.` `?.` `[ ]` lời gọi `( )`, `++` `--` hậu tố | trái |
| 2 | `!` `-` `+` `typeof` `void` `new`, `++` `--` tiền tố | tiền tố |
| 3 | `*` `/` `%` | trái |
| 4 | `+` `-` | trái |
| 5 | `<` `<=` `>` `>=` | trái |
| 6 | `==` `!=` `===` `!==` | trái |
| 7 | `&&` | trái |
| 8 | `\|\|` `??` (không trộn khi không có ngoặc) | trái |
| 9 | `? :`, `=` `+=` `-=` `*=` `/=`, `=>` | phải |

**Thứ tự tính.** Toán hạng và đối số tính từ trái sang phải; với `o[k] = v`, `o` rồi `k` rồi `v`.

**Số học và `+`.** `+ - * / %` theo phép đổi kiểu của JavaScript (mục 1.4): `"3" * "4"` là 12, `"10" - 1` là 9,
`[1, 2] + [3]` là `"1,23"`, `null + 1` là 1, `1 / 0` là `Infinity`, `5 % 0` là `NaN`, `-7 % 3` là -1. `+` nối chữ khi một
vế (sau khi đổi về giá trị nguyên thuỷ) là chuỗi; object đổi qua `valueOf` trước `toString`.

**Một ngôi.** `+x` là `Number(x)`; `-x` đổi dấu (`-null` là `-0`); `!x` phủ định giá trị thật giả (mục 1.4); `void e`
tính `e` rồi cho `undefined`. `typeof x` cho `"undefined"`, `"boolean"`, `"number"`, `"string"`, `"function"` hay
`"object"` (kể cả `null`, mảng, thời gian); `typeof` một tên chưa khai là `"undefined"`, không lỗi, như JavaScript.

**`++` và `--`** dùng trên một tên, `o.x` hay `o[k]`; object và khoá tính một lần (`a[i++]++`). Giá trị cũ được đổi thành
số trước: `x++` cho số cũ, `++x` cho số mới (`x = "5"; x++` cho 5, `x` thành 6). Xuống dòng trước `++` kết thúc biểu thức:
`x⏎++y` là `x; ++y`. `++x++` là lỗi biên dịch.

**So sánh.** Hai chuỗi so theo đơn vị UTF-16; vế khác loại so theo số (`"10" < 9` là `false`, `null >= 0` là `true`);
mảng so theo chữ (`[10] < [9]` là `true`). `===`, `!==` so chặt; `==`, `!=` so lỏng như JavaScript (`1 == "1"`,
`null == undefined` là `true`; `null == 0` là `false`). Object và mảng bằng nhau khi là cùng một giá trị (`[] == []` là
`false`); riêng thời gian so theo giá trị (mục 8).

**`&&`, `||`, `??`** trả về toán hạng quyết định và dừng sớm. `??` chỉ coi `null`, `undefined` là vắng (`0 ?? 1` là 0).
Trộn `??` với `&&` hay `||` không ngoặc là lỗi biên dịch, như JavaScript: `không trộn ?? với && hay || khi không có
ngoặc (JavaScript cũng báo lỗi): viết (a ?? b) || c hoặc a ?? (b || c)`.

**Gán.** `=` cho giá trị được gán, kết hợp phải (`a = b = 3`). Vế trái là một tên, `o.x`, `o[k]` hay một mẫu tách
(mục 3.6); thứ khác là lỗi biên dịch (`không gán được vào 5: vế trái của = là một tên, o.x, o[k] hay một mẫu tách
{ … }, [ … ]`). Gán gộp chỉ có `+=`, `-=`, `*=`, `/=`, vế trái tính một lần. Gán vào `const`, tên import hay biến của
`for (const …)` là lỗi biên dịch nêu chỗ khai (mục 2.2). Gán qua `?.`
(`o?.a = 1`) là lỗi biên dịch, như JavaScript.

**Thành viên.** `o.x`, `o[k]`: khoá vắng đọc ra `undefined`; đọc thành viên của `null` hay `undefined` là lỗi `TYPE`
(`cannot read properties of null (reading 'a')`). `?.`, `?.[ ]`, `?.( )` dừng cả chuỗi ở `undefined` khi vế trái là
`null` hay `undefined`, đối số không được tính; ngoặc kết thúc chuỗi (`(o?.a).b` đọc `.b` của `undefined`).

**Lời gọi.** Gọi thứ không phải hàm là lỗi `TYPE` lúc chạy (`x không phải hàm (là number): không gọi được`);
thông báo không bao giờ in giá trị. Một lời gọi viết tối đa 255 đối số (lỗi biên dịch), trải tối đa 65 535
(`INVALID_ARGUMENT` lúc chạy).

**Trải `...`.** Trong mảng và lời gọi: mảng, chuỗi (theo code point) và bytes; thứ khác là lỗi `TYPE` ở `...`. Trong
object: các khoá riêng theo thứ tự; `null`, `undefined`, số, boolean, hàm không thêm gì; chuỗi và mảng thêm theo chỉ số.

**`new`** chỉ dùng với `Date`, `Array`, `Object`, và là lời gọi của chúng: `new Date(x)` là thời gian của `x` (`Date(x)` không
`new` là lỗi biên dịch, mục 4.3), `new Array(3)` là ba
phần tử `undefined`, `new Object()` là `{}`; `new Date` không ngoặc là `new Date()`. Kitwork không có hàm dựng của chương
trình (không class, không `this`) và không có object bọc, nên `new` trên thứ khác (`new f()`, `new o.b()`,
`new String(5)`) là lỗi biên dịch; `new Date.now()` (JavaScript đọc là `new (Date.now)`) cũng vậy.

**Chuỗi mẫu.** `` `a${x}b` `` nối các phần bằng `String(…)` (object đổi qua `toString`); chuỗi mẫu lồng được và giữ dòng
mới.

**Object viết tay.** Khoá là một tên (kể cả từ khoá), một chuỗi, một số hay đúng một biểu thức trong ngoặc vuông `[k]`
(khoá đổi thành chuỗi trước khi tính giá trị); viết tắt `{ a }` chỉ dùng với một tên; trải `...o`. Thứ khác ở chỗ khoá
(`{ 1 + 1: v }`, `{ o.b: v }`, `{ -1: v }`, `{ x + 1 }`, `{ [a, b]: v }`) là lỗi biên dịch, như JavaScript; khoá trùng thì khoá sau thắng; dấu phẩy cuối được. Khoá số nguyên đứng trước (mục 1.5).
`__proto__` là một khoá thường. Không có method viết tắt `{ f() { … } }` và getter, setter: lỗi biên dịch, viết
`{ f: () => … }`.

**Mảng viết tay.** Không có lỗ trống (`[1, , 3]`) và dấu phẩy cuối (`[1, 2,]`): lỗi biên dịch.

**Kiểm lúc biên dịch.**
- Gọi thành viên mà builtin không có (mục 4.4).
- `x.safe()` không đối số là lỗi: viết `safe(() => x)` (mục 5).
- `safe` không nhận đúng một hàm: `safe nhận một hàm: viết safe(() => 1)`; tắt khi chương trình tự khai `safe`.
- `fail` (tên cũ của `error`, tới 07/11/2026) là một tên chưa khai như mọi tên khác (mục 2.2).
- Tên `kitwork` không khai, không gán được.

**Không có** (lỗi biên dịch nêu toán tử và cách viết thay, như `Kitwork không có **: viết Math.pow(a, b)`, 19/11/2026):
`**`, `**=` (viết `Math.pow`), toán tử bit `& | ^ ~ << >> >>>` và phép gán của chúng, `%=` và các phép gán gộp khác ngoài
bốn cái trên, `&&=`, `||=`, `??=` (viết `a = a ?? b`), toán tử dấu phẩy (`Kitwork không có toán tử dấu phẩy: … mỗi việc viết thành một
câu lệnh`), `in` (`Object.hasOwn(o, k)`), `instanceof` (`Array.isArray(x)`), `import(…)`, `new.target`, `async`,
`await`, `yield`, `this`, `super`.

**`delete`.** `delete o.k`, `delete o[k]` theo nghĩa JavaScript strict: xoá khoá riêng của một object, là `true` khi khoá
không còn (không có thì cũng `true`). Lỗi `TYPE`, như strict mode ném: khoá có thật của object đã đóng băng
(`Object.freeze`, dữ liệu file làm lúc boot, mục 1.7), `length` của mảng, chỉ số hay `length` của chuỗi, bytes,
`length` và `name` của hàm, đọc từ `null`, `undefined`. Phần tử của mảng là lỗi `UNSUPPORTED`: JavaScript để lại một chỗ
trống, mảng của Kitwork không có chỗ trống (`a.splice(i, 1)`, hay `a[i] = undefined`). Lỗi biên dịch: `delete x` (strict
JavaScript cũng từ chối), `delete` một thứ không phải khoá (`delete 5`), `delete o?.k`.

### 3.4. Câu lệnh

```ebnf
Statement      = Block | Declaration | FunctionDecl | ImportDecl | ExportDecl | IfStmt
               | SwitchStmt | ForStmt | ReturnStmt | BreakStmt | ContinueStmt | ExpressionStmt | ";" .
Block          = "{" { Statement } "}" .
IfStmt         = "if" "(" Expression ")" Body [ "else" Body ] .
Body           = Block | Statement .
SwitchStmt     = "switch" "(" Expression ")" "{" { CaseClause } "}" .
CaseClause     = ( "case" Expression | "default" ) ":" { Statement } .
ForStmt        = CountedFor | ForOf .
CountedFor     = "for" "(" ( "let" | "const" ) identifier "=" Expression ";" Condition ";" Update ")" Block .
ForOf          = "for" "(" ( "let" | "const" ) ( identifier | Pattern ) "of" Expression ")" Block .
ReturnStmt     = "return" [ Expression ] .
BreakStmt      = "break" .
ContinueStmt   = "continue" .
ExpressionStmt = Expression .
```

**Kết thúc câu lệnh.** Như JavaScript, một câu lệnh kết thúc ở `;`, ở chỗ xuống dòng mà dòng sau không nối tiếp được
nó, ở `}` hay ở cuối file; câu lệnh kết thúc bằng khối của chính nó (`if`, `for`, `switch`, khai báo `function`, một khối)
không cần gì sau. Hai câu lệnh trên một dòng phải ngăn bằng `;`: `const r = 1 2` là lỗi biên dịch (`cần ; hay xuống
dòng trước "2": hai câu lệnh trên một dòng ngăn nhau bằng ;`). Xuống dòng trước `++`, `--` kết thúc câu lệnh; `break`,
`continue` kết thúc ở chỗ xuống dòng. `return` theo sau là xuống dòng là `return;`, như JavaScript: giá trị phải bắt đầu
trên dòng của `return` (`return (` rồi xuống dòng thì được). Một khối chưa đóng tới hết file là lỗi biên dịch (`khối mở
bằng { ở đây chưa đóng: thiếu } trước khi hết file`).

**`if`.** `if (c) a else b`, `else if` lồng, `else` gắn với `if` gần nhất. `if` là câu lệnh, không có giá trị: ở chỗ của
một giá trị (`const r = if …`) là lỗi biên dịch; viết `c ? a : b`. Thân là một khối hay một câu lệnh; thân trần
kết thúc như mọi câu lệnh, nên `if (c) x = 1; else x = 2` đọc được, còn `if (c) x = 1 else x = 2` trên một dòng là lỗi,
như JavaScript. Thân rỗng `if (c) ;` là lỗi biên dịch: dùng khối.

**`switch`.** So bằng `===` (`"1"` không khớp `1`, `NaN` không khớp `NaN`); biểu thức của `case` tính lần lượt tới khi
khớp; rơi xuống `case` sau khi không `break`; `default` đứng ở đâu cũng được, một lần (`switch chỉ được có một
default`). Mọi `case` chung một khối: hai `case` cùng khai `const a` là khai lại, bọc mỗi `case` trong `{ … }` thì được.

**Vòng đếm.** Chỉ một dạng, để vòng lặp có số lượt đếm được:
- biến đếm khai bằng `let` (khai bằng `const` thì bước nhảy là gán vào hằng);
- điều kiện là `<`, `<=`, `>`, `>=` hay `!=` với biến đếm là một vế (`i < n`, `5 > i`, `i < a.length`); thứ khác là lỗi
  biên dịch: `Điều kiện for phải so sánh biến đếm 'i' (ví dụ 'i < n', 'i >= 0'). Điều kiện tuỳ ý là 'while' trá hình và
  không được phép.`;
- bước nhảy là `i++`, `++i`, `i--`, `--i`, `i += e`, `i -= e`, `i *= e`, `i /= e`, `i = e`; thứ khác là lỗi (`Bước nhảy
  for phải cập nhật biến đếm 'i' (i++, i--, i += n).`);
- thân là một khối.

Dạng ấy không chứng minh vòng dừng (`i--` với `i < 3`, gán `i = 0` trong thân): năng lượng (mục 7) là chốt chặn.
`for (;;)`, `for (; c ;)`, nhiều biến đếm (`let i = 0, j = 0`) là lỗi biên dịch.

**`for…of`** lặp mảng, chuỗi (theo code point) và bytes; mảng đổi trong lúc lặp thì vòng thấy, như JavaScript. Đầu vòng
là `let` hay `const` với một tên hay một mẫu tách (`for (const [k, v] of Object.entries(o))`). Thứ khác là lỗi `TYPE`
lúc chạy: `for…of: một object không lặp được; lặp các khoá, giá trị hay cặp của nó: …`, `for…of: number không lặp
được: chỉ lặp mảng, chuỗi hay bytes`.

**`break`, `continue`.** `break` trong `switch` hay vòng `for`; `continue` trong vòng `for` (trong một `switch` nằm
trong vòng thì đi tiếp vòng). Không vượt qua ranh giới hàm. Không có nhãn: `break x`, `continue x`, `nhãn: …` là lỗi
biên dịch.

**`return`** kết thúc hàm, trả `undefined` khi không có giá trị. Ở cấp cao nhất của file là lỗi biên dịch (`return chỉ
đứng trong một hàm`), như module của JavaScript: cấp cao nhất chạy tới cuối.

**Không có** (lỗi biên dịch, mỗi lỗi nói cách viết thay):
- `while`, `do … while`: `Kitwork không hỗ trợ vòng điều kiện tuỳ ý 'while' (có thể lặp vô tận). Hãy dùng vòng ĐẾM
  'for (let i = 0; i < n; i++)', duyệt 'for (const x of arr)', hoặc .map()/.filter()/.find() trên mảng.`;
- `try`, `catch`, `finally`, `throw`: báo lỗi bằng `return error("…")`, thu lỗi bằng `safe(() => …)` (mục 5);
- `class`: dùng object viết tay và hàm mũi tên;
- `for…in`: `for (const k of Object.keys(o))`;
- `with`, `debugger`, nhãn.

### 3.5. Hàm

```ebnf
FunctionDecl   = "function" identifier Parameters Block .
FunctionExpr   = "function" [ identifier ] Parameters Block .
ArrowFunction  = ( identifier | Parameters ) "=>" ( Block | Expression ) .
Parameters     = "(" [ Param { "," Param } [ "," RestParam ] | RestParam ] ")" .   /* không dấu phẩy cuối */
Param          = ( identifier | Pattern ) [ "=" Expression ] .
RestParam      = "..." identifier .
```

- Ba dạng: khai báo `function f(a) { … }`, biểu thức `function (a) { … }`, mũi tên `(a) => a + 1` hay `(a) => { … }`.
- Biểu thức có tên, `const f = function g(n) { … }`: trong thân, `g` là chính hàm ấy (gọi đệ quy được, gán là lỗi biên
  dịch), ngoài thân không có `g`; `f.name` là `"g"`.
  Mũi tên trả về một object phải bọc ngoặc: `(u) => ({ id: u.id })`; viết `(u) => { id: u.id }` là lỗi biên dịch.
- Mũi tên là cả một biểu thức, như JavaScript: đối số, giá trị, vế phải của `=`, nhánh của `? :`; làm toán hạng của một
  toán tử thì bọc ngoặc (`1 + a => 2` là lỗi biên dịch). Tham số và `=>` cùng một dòng. `()` rỗng chỉ là tham số của
  `() => …`.
- **Mặc định của tham số** áp khi đối số vắng hay là `undefined`, không cho `null`. Nó đứng trong phạm vi của các tham số,
  như JavaScript: đọc được tham số đứng trước nó; đọc chính nó hay tham số đứng sau là lỗi biên dịch (JavaScript ném
  `ReferenceError`); không thấy tên khai trong thân hàm (một hàm trong mặc định đọc biến bên ngoài cùng tên).
- **Phần còn lại** `(a, ...rest)` là tham số cuối, không có mặc định, là `[]` khi không có đối số thừa.
- **Tham số tách:** `({ a, b = 2 }) =>`, `([x, [y]]) =>`, `({ a } = { a: 9 }) =>`.
- Hai tham số cùng tên là lỗi biên dịch.
- `length` của hàm đếm tham số đứng trước tham số có mặc định đầu tiên và trước phần còn lại, như JavaScript; `name`
  lấy từ tên khai, tên biến hay khoá giữ hàm (`const bar = () => 1` có `name` là `"bar"`).
- Không có `this`, `arguments`, `new.target`, hàm sinh (`function*`, `yield`), `async`, `await`: dùng các tên ấy là lỗi
  biên dịch (tên chưa khai, hay lỗi cú pháp). Việc chạy song song và chờ đi qua `kitwork/routine`; việc nền qua
  `kitwork/queue`, `kitwork/schedule`.
- Không có method viết tắt trong object (`{ f() { … } }`), getter, setter.
- Đệ quy tối đa 64 khung gọi kể cả cấp cao nhất (mục 7).

### 3.6. Mẫu tách

```ebnf
Pattern        = ObjectPattern | ArrayPattern .
ObjectPattern  = "{" [ PropPattern { "," PropPattern } [ "," ] ] "}" .
PropPattern    = identifier [ "=" Expression ]
               | PropKey ":" BindingTarget [ "=" Expression ]
               | "..." identifier .                         /* cuối cùng, không mặc định */
PropKey        = IdentifierName | string_lit .
ArrayPattern   = "[" [ ArrayElement { "," ArrayElement } [ "," ] ] "]" .
ArrayElement   = BindingTarget [ "=" Expression ] | "..." BindingTarget .
BindingTarget  = identifier | Pattern .
```

Mẫu tách dùng trong khai báo (`const { a, b: c } = o`), phép gán (`[a, b] = [b, a]`, `({ a } = o)`), tham số và đầu
`for…of`.
- Mặc định áp khi giá trị đọc là `undefined`, không cho `null` (`const { p = 1 } = { p: null }` ra `null`).
- Tách một mẫu object từ `null`, `undefined` là lỗi `TYPE` (`cannot read properties of null (reading 'a')`).
- Mẫu mảng đọc theo phép lặp, như JavaScript: một mảng là các phần tử của nó, một chuỗi là các ký tự (code point: `const
  [a] = "😀"` là `"😀"`), bytes là các số; giá trị khác (`null`, số, object, kể cả object có `length`) là lỗi `TYPE` ở
  mẫu (`mẫu mảng [ … ] tách theo phép lặp: number không lặp được; chỉ mảng, chuỗi hay bytes`).
- Phần còn lại của mẫu mảng là một mẫu được (`const [a, ...[b, c]] = list`); của tham số thì không: `(...[a, b]) =>` là
  lỗi biên dịch.
- Chưa có (lỗi biên dịch nêu cách viết): chỗ trống `[, b]`, khoá tính `{ [k]: v }`, khoá số `{ 0: a }`, gán vào `o.x`
  khi tách.

### 3.7. Kiểm `null` lúc biên dịch

Một giá trị mà lời gọi thư viện có thể trả `null`, bị dùng ở chỗ JavaScript ném khi gặp `null` trên một đường chưa kiểm
nó, là lỗi biên dịch (chốt 27/10/2026, làm lát 1 ngày 19/11/2026; `core/compiler/nullcheck.go`, bản nháp
`docs/KITWORK_NULL_CHECK.md` của repo gốc):

```js
import * as file from "kitwork/file";
const settings = file.json("settings.json");
const theme = settings.theme;
// router.work:3:15: settings có thể là null: file.json(…) trả null khi không có file; kiểm trước khi đọc
//   (if (settings == null) return …), hay viết settings?.…
```

- **Lời gọi có thể ra `null`** (tập đóng): `read`, `text`, `bytes`, `json`, `info` của `kitwork/file`; `get()` không đối số
  của một handle `kitwork/storage` (`memory(…)`, `disk(…)`, `datastore(…)`, qua `segment`, `expire`); `first()` của một
  câu lệnh trên `connection()` của `kitwork/database`; `context.request.file(tên)` trong hàm của một cửa router;
  `error` của kết quả `safe(…)`, là `null` khi hàm chạy xong.
- **Dùng:** đọc hay ghi thành viên (`x.y`, `x[k]`, `x.y = 1`), gọi method, tách (`const { a } = x`), `for…of`, trải vào
  mảng hay lời gọi. Truyền đi, so sánh, trả về, `{ ...x }`, `String(x)`, `JSON.stringify(x)` không phải dùng.
- **Kiểm:** `if (x)`, `if (x != null)`, `if (x !== null)`, nhánh `else` của `if (x == null)`, sau `if (!x) return …`
  (`return`, `break`, `continue`), `x && x.y`, `x ? x.y : d`, `x == null || x.y`, gộp bằng `&&`, `||`; `x?.y` luôn được;
  `x ?? d` cho một giá trị không `null`. `typeof x === "object"` và `x !== undefined` không kiểm.
- **Dòng chảy:** sau `if`/`else`, đã kiểm khi mọi nhánh đi tiếp đều đã kiểm. Một `let` gán lại một giá trị có thể `null`
  thì chưa kiểm lại; trong vòng lặp, lượt sau đọc cái lượt trước gán. Một hàm viết sau phép kiểm giữ phép kiểm của một
  `const` (hằng không đổi), không giữ của một `let` (hàm có thể chạy sau khi `let` đổi); `function` được đưa lên đầu nên
  không giữ phép kiểm nào.
- **Lát 1** theo một giá trị trong một file: từ lời gọi tới tên giữ nó, qua hàm chứa nó và các hàm viết bên trong. Giá trị
  đi qua `return` của một hàm khác hay `export` của file khác chưa được theo (lát 2, 3: mục 9). Biểu thức `.ui` không bị kiểm:
  markup đọc thành viên của `null` ra rỗng.
- Không có cách nói "biết nó không `null`" (`x!` của TypeScript không phải JavaScript): viết phép kiểm.

## 4. Thư viện sẵn có (`core/builtins`)

Mọi file có sẵn 21 tên, không cần import, theo thứ tự chỗ (`BUILTIN n`, `core/builtins/builtins.go`): `Math`, `JSON`,
`console`, `Number`, `String`, `Boolean`, `Array`, `Object`, `Date`, `parseInt`, `parseFloat`, `isNaN`, `isFinite`,
`encodeURIComponent`, `decodeURIComponent`, `encodeURI`, `decodeURI`, `Infinity`, `NaN`, `error`, `safe`. Chỗ 0 là
`kitwork`, host điền. `undefined` là từ khoá, không phải tên (mục 3.2).

- Mọi builtin bị đóng băng tới tận cùng: `Math.PI = 3` là lỗi `TYPE` (`cannot assign to read only property 'PI' of
  object '#<Object>'`), `Math.x = 1` cũng vậy (`cannot add property x, object is not extensible`).
- Một khai báo che được tên builtin (`const NaN = 5`, `const Math = { … }`), như JavaScript; gán vào tên builtin
  (`console = null`) là lỗi biên dịch (mục 2).
- Không builtin nào có `prototype` hay `constructor`: `Number.prototype` đọc ra `undefined`.
- `length` và `name` của hàm builtin theo JavaScript (`Math.max.length` là 2, `Date.length` là 7).

### 4.1. Đối tượng và hàm toàn cục

| Tên | `typeof` | Thành viên |
|---|---|---|
| `Math` | `object` | `abs`, `acos`, `acosh`, `asin`, `asinh`, `atan`, `atanh`, `atan2`, `cbrt`, `ceil`, `clz32`, `cos`, `cosh`, `exp`, `expm1`, `floor`, `fround`, `hypot`, `imul`, `log`, `log1p`, `log2`, `log10`, `max`, `min`, `pow`, `random`, `round`, `sign`, `sin`, `sinh`, `sqrt`, `tan`, `tanh`, `trunc`; hằng `E`, `LN2`, `LN10`, `LOG2E`, `LOG10E`, `PI`, `SQRT1_2`, `SQRT2` |
| `JSON` | `object` | `parse(text, reviver?)`, `stringify(value, replacer?, indent?)` |
| `console` | `object` | `log`, `info`, `warn`, `error`, `debug` |
| `Number` | `function` | `Number(x)`; `isFinite`, `isInteger`, `isNaN`, `isSafeInteger`, `parseFloat`, `parseInt`; `MAX_VALUE`, `MIN_VALUE`, `MAX_SAFE_INTEGER`, `MIN_SAFE_INTEGER`, `EPSILON`, `NaN`, `POSITIVE_INFINITY`, `NEGATIVE_INFINITY` |
| `String` | `function` | `String(x)`; `fromCharCode`, `fromCodePoint` |
| `Boolean` | `function` | `Boolean(x)` |
| `Array` | `function` | `Array(n)`, `Array(a, b, …)`; `isArray`, `from`, `of` |
| `Object` | `function` | `Object(x)`; `assign`, `entries`, `freeze`, `fromEntries`, `getOwnPropertyNames`, `groupBy`, `hasOwn`, `is`, `isFrozen`, `keys`, `values` |
| `Date` | `function` | `new Date()`, `new Date(x)`, `new Date(năm, tháng, …)`; `now`, `parse`, `UTC` |
| `parseInt`, `parseFloat` | `function` | chính là `Number.parseInt`, `Number.parseFloat`; `parseInt` quá 2^53 ở cơ số không phải 10 hay luỹ thừa của 2 là lỗi `RANGE` (19/11/2026: JavaScript để mỗi engine tự làm tròn chữ số cuối, Kitwork không chép cách của V8) |
| `isNaN`, `isFinite` | `function` | đổi đối số thành số trước, như JavaScript (`Number.isNaN` thì không) |
| `encodeURIComponent`, `decodeURIComponent`, `encodeURI`, `decodeURI` | `function` | chuỗi hỏng là lỗi `INVALID_INPUT` (`URI malformed: …`) |
| `Infinity`, `NaN` | `number` | gập thành hằng lúc biên dịch |
| `error`, `safe` | `function` | mục 5 |

`new` chỉ dùng với `Date`, `Array`, `Object`, và là lời gọi của chúng (mục 3.3); `new String("x")` là lỗi biên dịch: không
có object bọc.

### 4.2. Method trên giá trị

Một lời gọi `x.m(…)` tìm theo thứ tự (`core/value/operation.go`): khoá riêng của object hay hàm, rồi method của loại giá
trị. Khoá riêng không phải hàm là lỗi `TYPE` (`a is not a function`); loại không có method ấy là lỗi `TYPE`
(`string has no method match`). Mọi loại trừ `null`, `undefined` có `hasOwnProperty`, `toString`, `valueOf`.

| Loại | Method |
|---|---|
| chuỗi | `at`, `charAt`, `charCodeAt`, `codePointAt`, `concat`, `endsWith`, `includes`, `indexOf`, `isWellFormed`, `lastIndexOf`, `padEnd`, `padStart`, `repeat`, `replace`, `replaceAll`, `slice`, `split`, `startsWith`, `substr`, `substring`, `toLowerCase`, `toUpperCase`, `toWellFormed`, `trim`, `trimStart`, `trimEnd` (`trimLeft`, `trimRight` là chính hai hàm này); thuộc tính `length` |
| số | `toExponential`, `toFixed`, `toPrecision`, `toString(cơ số)` |
| boolean | chỉ ba method chung |
| mảng | `at`, `concat`, `copyWithin`, `every`, `fill`, `filter`, `find`, `findIndex`, `findLast`, `findLastIndex`, `flat`, `flatMap`, `forEach`, `includes`, `indexOf`, `join`, `lastIndexOf`, `map`, `pop`, `push`, `reduce`, `reduceRight`, `reverse`, `shift`, `slice`, `some`, `sort`, `splice`, `toReversed`, `toSorted`, `toSpliced`, `unshift`, `with`; thuộc tính `length` (chỉ đọc) |
| object | ba method chung, cộng các khoá riêng là hàm |
| hàm | ba method chung; thuộc tính `length`, `name`. Không có `call`, `apply`, `bind` |
| thời gian (của `Date`) | `getTime`, `toISOString`, `format(layout)` |
| khoảng thời gian | `hours`, `minutes`, `seconds`, `milliseconds` |
| bytes | ba method chung; thuộc tính `length`, `[i]` là một số |

**Giá trị thường chỉ có method của JavaScript** (chuỗi, số, mảng, object): không method nào Kitwork tự thêm. Method của
Kitwork chỉ ở hai loại của Kitwork: `format` của thời gian, `hours`/`minutes`/`seconds`/`milliseconds` của khoảng thời
gian. Object của một module (`context`, bảng database, `file`…) có method của nó, như host object của trình duyệt, tả ở
tài liệu của module ấy; chúng không có ba method chung.

Một method chạy trên giá trị nó được gọi: `s.toUpperCase()`, `s["toUpperCase"]()`, `(s.toUpperCase)()`, `s.at?.(-1)` như
JavaScript. Đọc ra mà không gọi thì là một hàm không có giá trị để làm việc: gọi riêng là lỗi `TYPE` (`method toUpperCase đọc
ra khỏi giá trị rồi gọi riêng: …; gọi thẳng x.toUpperCase(…), hay viết (x) => x.toUpperCase(…)`), như JavaScript ném
`TypeError`; `[].map === [].map` là `true`, `typeof s.at` là `"function"`. Gọi qua một khoá từ biến, `s[k]()` với `k` là tên
một method của giá trị, cũng là lỗi ấy (JavaScript chạy được; mục 8.4); hàm giữ trong object hay mảng (`handlers[k](x)`)
thì gọi được.

### 4.3. Chi tiết theo nhóm

**Chuỗi.** `split`, `replace`, `replaceAll` chỉ nhận chữ (đối số đổi thành chuỗi); không có biểu thức chính quy, nên
không có `match`, `matchAll`, `search`. `replace` thay lần đầu; mẫu thay `$$`, `$&`, `` $` ``, `$'` chạy, `$1` giữ
nguyên chữ; hàm thay nhận `(chuỗi khớp, vị trí, chuỗi gốc)`. Đổi hoa thường như JavaScript, theo đủ bảng của Unicode
(như node, Unicode 16): một ký tự đổi thành bao nhiêu ký tự cũng được (`"ß".toUpperCase()` là `"SS"`, `"İ".toLowerCase()`
là `"i̇"`), `Σ` cuối từ thành `ς` (`"ΑΣ".toLowerCase()` là `"ας"`); nửa surrogate giữ nguyên. `trim` theo tập khoảng trắng của
JavaScript (kể cả NBSP, BOM). Không có `localeCompare`, `normalize`, `toLocaleUpperCase`, `toLocaleLowerCase`.

**Số và `Math`.** `Number(x)` như JavaScript (`"0x1f"` là 31, `"1_000"` là `NaN`). `toString(cơ số)` viết như node
(V8) từng chữ số: phần lẻ tới đúng độ chính xác của số, phần nguyên lớn có các chữ số dưới độ chính xác là 0
(`(1e21).toString(16)` là `"3635c9adc5dea00000"`); `toString(10)` là `String(n)`. Cơ số hay số chữ số ngoài khoảng là
lỗi `RANGE`. `Math.random` là bộ sinh chung của tiến trình (`math/rand/v2`), chương trình không gieo được.
Không có `toLocaleString`.

**Mảng.** `Array(n)` (19/11/2026) là mảng `n` chỗ trống của JavaScript: Kitwork giữ `undefined` ở đó, đọc ra như một chỗ
trống đọc ra (`a[i]`, `length`, `join`, JSON, `for…of`, `[...a]`, `Array.from`, `find`, `includes`, `toSorted`, `with`),
nên `Array(n).fill(0)`, `[...Array(n)].map((_, i) => i)`, `Array.from(Array(n), …)` là của JavaScript. Thứ bỏ qua chỗ
trống thì là lỗi `TYPE` trên một mảng còn chỗ trống (`a.map(…): mảng tạo bằng Array(n) còn chỗ trống, …`): `map`,
`forEach`, `filter`, `some`, `every`, `reduce`, `reduceRight`, `indexOf`, `lastIndexOf`, `flat`, `flatMap`,
`hasOwnProperty`, `Object.keys`, `values`, `entries`, `getOwnPropertyNames`, `hasOwn`, nguồn của `Object.assign`,
`{ ...a }`. Trước đó chúng cho giá trị khác (`Array(3).every((x) => false)` là `false`, JavaScript `true`). `slice`,
`concat`, `splice` giữ chỗ trống như JavaScript; `fill` cả mảng lấp hết, `fill` một phần thì không. Độ dài không hợp lệ
là `RANGE`; builtin không tạo mảng quá 2^24 phần
tử (`TOO_LARGE`). `sort()` mặc định so chữ của từng phần tử theo đơn vị UTF-16, `undefined` cuối cùng, như JavaScript;
sắp xếp ổn định; hàm so sánh không phải hàm là lỗi `TYPE`. Với hàm so nhất quán, sắp ổn định chỉ có một kết quả, nên luôn
khớp JavaScript. Với hàm so không nhất quán (`(a, b) => a % 3 - b % 2`), JavaScript để thứ tự tuỳ engine (V8 dùng
TimSort), nên không có nghĩa nào để theo: thứ tự là của thuật toán Kitwork, khác node, và không phát hiện được với chi phí
rẻ để báo lỗi (chủ dự án chốt 19/11/2026). Gán `a.length` là lỗi `UNSUPPORTED`. Không có `keys`,
`values`, `entries`, `toLocaleString`. `[].reduce(f)` không giá trị đầu là lỗi `TYPE`.

**Thư viện không phải dữ liệu** (19/11/2026): `Math`, `JSON`, `console` dùng như dữ liệu là lỗi `TYPE` (`String(Math):
Math là thư viện, không phải dữ liệu; đọc từng tên của nó (Math.PI)`): đổi thành chữ (`String(Math)`, `` `${JSON}` ``),
liệt kê khoá (`Object.keys`, `values`, `entries`), trải `{ ...Math }`, nguồn của `Object.assign`, `JSON.stringify` (kể cả
giá trị `replacer` trả). Trước đó `String(Math)` là `"[object Object]"` (JavaScript `"[object Math]"`), `Object.keys(JSON)`
là `["parse","stringify"]` (JavaScript `[]`). Thứ khớp JavaScript giữ nguyên: `Object.getOwnPropertyNames(Math)`,
`Object.hasOwn(Math, "PI")`, `+Math`.

**Object.** Kitwork không có object bọc giá trị nguyên thuỷ (19/11/2026): `Object(5)`, `Object("x")`, `new Object(true)`,
`Object.assign` một số, chuỗi hay boolean là lỗi `TYPE` (trước đó trả lại chính giá trị, `typeof` là `"number"` chỗ
JavaScript là `"object"`). `Object.assign` ghi khoá vào một mảng, thời gian hay hàm là lỗi `TYPE` (trước đó trả target
không ghi gì, `Object.assign([1, 2], [3])` là `[1, 2]` chỗ JavaScript là `[3, 2]`); không có gì để ghi thì trả target như
JavaScript. `Object(null)`, `Object()` là `{}`, `Object(o)` là `o`. `Object.freeze` nông; ghi vào object đã đóng băng là lỗi `TYPE`. `Object.keys(null)` là lỗi `TYPE`
(`cannot convert undefined or null to object`). Một hàm có riêng `length` và `name`: `Object.getOwnPropertyNames(f)`
là `["length", "name"]`, `Object.hasOwn(f, "name")` là `true`, `Object.keys(f)` là `[]`, như JavaScript; hàm khai bằng
`function` không có `prototype` (Kitwork không có hàm dựng). Không có `create`, `defineProperty`, `seal`, `getPrototypeOf`.
`Object.groupBy(items, f)` (19/11/2026) như JavaScript: object có khoá là cái `f(item, index)` trả về, đổi thành khoá như
JavaScript (số thành chữ của nó; thời gian, hàm không có chữ nên là lỗi `TYPE`), mỗi khoá giữ mảng các phần tử theo thứ tự;
`items` là thứ `for…of` lặp được (mảng, đọc dần nên phần tử hàm gọi lại thêm vào cũng được lặp; chuỗi theo code point;
bytes), thứ khác là lỗi `TYPE`. Không có `Map.groupBy` (Kitwork không có `Map`).

**`JSON`.** Khoá theo thứ tự của object (mục 1): khoá số nguyên trước, tăng dần, rồi thứ tự thêm vào. `undefined` và hàm
bị bỏ khỏi object, thành `null` trong mảng; `NaN`, `±Infinity` thành `null`; `stringify(undefined)` là `undefined`.
`toJSON` của một object được gọi với khoá của nó (`""` cho giá trị gốc, chỉ số dạng chữ trong mảng) và cái nó trả được
viết thay, trước `replacer`, như JavaScript; chỉ `JSON.stringify` của chương trình gọi nó, còn code Go của engine viết một
giá trị ra JSON (`context.json`) thì không chạy gì của chương trình. Thời gian viết thành `toISOString()`, khoảng thời gian thành chữ của nó (`"1h0m0s"`), bytes
thành base64, object của module thành `{}`. `replacer` là hàm hay mảng khoá; `indent` như JavaScript (số kẹp 0…10, chuỗi
lấy 10 đơn vị đầu). Object tự chứa mình là lỗi `TYPE` (`JSON: the object holds itself`). `parse` hỏng là `INVALID_INPUT`
nêu vị trí byte (`JSON: expected a key at byte 1`); lồng quá 513 tầng là lỗi; khoá trùng thì khoá sau thắng; `reviver`
chạy.

**`Date` và thời gian.** `new Date(…)` cho một giá trị thời gian (`typeof` là `"object"`), không phải object có getter,
setter: không có `getFullYear`, `getMonth`, `setFullYear`, `toJSON`, `getTimezoneOffset`… So hai thời gian bằng `===`,
`!==`, `==`, `!=` (và `switch` trên thời gian) là lỗi `TYPE` (`hai thời gian không so bằng ===: …; so thời điểm thì
a.getTime() === b.getTime()`): JavaScript so hai object `Date` theo chỗ (`new Date(0) === new Date(0)` là `false`), điều mà
thời gian của Kitwork, một giá trị, không nói được; `t === t` cũng là lỗi ấy. So thứ tự (`<`, `>`) và so với `null` vẫn
được. `getTime()`, `valueOf()`, `+t` là số mili giây từ 1970
UTC. `toISOString()` luôn ba chữ số lẻ. `toString()`, `String(t)`, `t + ""` là lỗi `TYPE` (`một thời gian không đổi thành
chữ: …`, mục 1.4): JavaScript viết theo múi giờ của máy chạy (`"Thu Jan 01 1970 07:00:00 GMT+0700 (…)"`), Kitwork không
viết thay; `JSON.stringify(t)` vẫn là `toISOString()`. `format(layout)`
nhận layout của Go (`"2006-01-02 15:04"`) và luôn viết theo UTC. Ngày không đọc được là lỗi `RANGE` (`invalid date:
hello`), cố ý khác JavaScript: Invalid Date làm `NaN` lan đi không báo (mục 8.4); `Date.parse("hello")` là `NaN`; thời gian
ngoài khoảng 1678–2262 là lỗi `RANGE`. `Date(…)` gọi không `new` là lỗi biên dịch (`Date(…) không new: JavaScript bỏ các đối
số và trả chuỗi của giờ hiện tại; tạo thời gian thì new Date(…), …`): JavaScript cho chuỗi của giờ hiện tại, gần như luôn
là viết nhầm.

Chữ chỉ có ngày đọc theo UTC; chữ có giờ mà không có múi, và `new Date(năm, tháng, …)`, đọc theo múi của nơi chạy:

| Nơi chạy | Múi |
|---|---|
| request, job, lịch của một site | `module.timezone` của app, không có thì `app.timezone` của host, không có thì UTC |
| `module.work`, `design.work` và `config.work` ở gốc app | UTC |
| `application.work`, `app.commands` | múi của máy chạy host |

Phép tính: thời gian − thời gian = khoảng thời gian; thời gian ± khoảng thời gian = thời gian; khoảng thời gian × hay ÷
số = khoảng thời gian; khoảng thời gian ÷ khoảng thời gian = số; thời gian − số = số; thời gian + số nối chữ.
`d.hours()`, `d.minutes()`, `d.seconds()` trả số có phần lẻ; chữ của khoảng thời gian là dạng của Go (`"1h30m2s"`).

**`console`.** Mỗi lời gọi viết một dòng: chữ của các đối số nối bằng khoảng trắng (object là `"[object Object]"`, không
đọc `%s`). Ở một site, dòng vào log của host với đầu `[<site>] `; `error`, `warn` ở mức của chúng, `debug` ở mức debug,
`log` và `info` ở mức info. Ký tự điều khiển bị thoát, mỗi dòng cắt ở 64 KB.

### 4.4. Tên của JavaScript không có

`globalThis`, `Map`, `Set`, `WeakMap`, `WeakSet`, `Symbol`, `Promise`, `Proxy`, `Reflect`, `Intl`, `RegExp`, `Error` và
các lớp lỗi, `BigInt`, `structuredClone`, `setTimeout` và các hẹn giờ, `queueMicrotask`, `fetch`, `TextEncoder`, `URL`,
`eval`, `Function`, `escape`, `window`, `process`, `require` không có. Dùng một tên ấy là lỗi biên dịch tên chưa khai
(mục 2): `Map chưa được khai: khai nó bằng const hay let, hay import nó; JavaScript ném ReferenceError ở đây`; `typeof Map`
là `"undefined"`. Mạng, file, database, thời gian chờ đến qua module (`kitwork/http`, `kitwork/file`,
`kitwork/database`, `kitwork/routine`, mục 6).

Gọi một thành viên mà builtin không có là lỗi biên dịch, kèm tên gần nhất khi có: `Math.sqr(2)` báo `có phải
Math.sqrt?`, `Object.seal(o)` báo `Object không có seal, nên Object.seal(…) không gọi được`. Phép kiểm chỉ thấy lời gọi
viết thẳng `Builtin.thành_viên(…)`; đọc không gọi (`Math.sqr`) ra `undefined`, còn gọi qua ngoặc vuông (`Math["sqr"](2)`)
hay qua một tên khác (`const m = Math; m.sqr(2)`) là lỗi `TYPE` lúc chạy.

**Tên tĩnh của JavaScript mà builtin của Kitwork chưa có** (19/11/2026): đọc một tên builtin cùng tên của JavaScript có
mà Kitwork không có là lỗi `TYPE`, đọc thẳng hay qua tên khác (`const A = Array; A.prototype`); trước đó ra `undefined`,
khác JavaScript (`Array.prototype` là `[]`, `typeof Array.prototype` là `"object"`). `Object.hasOwn(b, tên)`,
`b.hasOwnProperty(tên)` với một tên ấy, và `Object.getOwnPropertyNames(b)` của builtin thiếu tên cũng là lỗi đó. Các tên:
`prototype` của `Array`, `Object`, `Number`, `String`, `Boolean`, `Date`; `Array.fromAsync`, `String.raw`;
`Object.create`, `defineProperty`, `defineProperties`, `getOwnPropertyDescriptor`, `getOwnPropertyDescriptors`,
`getOwnPropertySymbols`, `getPrototypeOf`, `setPrototypeOf`, `preventExtensions`, `isExtensible`, `seal`, `isSealed`;
`JSON.rawJSON`, `isRawJSON`; của `console` theo chuẩn console: `assert`, `clear`, `count`, `countReset`, `dir`, `dirxml`,
`group`, `groupCollapsed`, `groupEnd`, `table`, `time`, `timeEnd`, `timeLog`, `trace`. `Math` có đủ tên của JavaScript.
Tên cả hai không có (`Array.foo`) vẫn là `undefined`; hàm của chương trình, dù tên `Array`, không phải builtin.

**Method của JavaScript mà Kitwork chưa có** (19/11/2026): một tên prototype của JavaScript có cho loại giá trị ấy mà
Kitwork không có là lỗi `TYPE` khi đọc, như khi gọi (`array của Kitwork chưa có entries (JavaScript có): đọc hay gọi nó là
lỗi`); trước đó đọc ra `undefined`, khác hàm của JavaScript (`[1].entries ? 1 : 2` là 2). Các tên ấy: của chuỗi `match`,
`matchAll`, `search`, `normalize`, `localeCompare`, `toLocaleLowerCase`, `toLocaleUpperCase` và các method HTML (`anchor`,
`bold`, `link`, …); của số `toLocaleString`; của mảng `entries`, `keys`, `values`, `toLocaleString`; của thời gian các getter,
setter của `Date` (`getFullYear`, `getUTCDay`, `setMonth`, …), `toJSON`, `toUTCString`, `toLocaleString` và các `to…String`
khác; của hàm `call`, `apply`, `bind`; của mọi loại (Object.prototype) `constructor`, `__proto__`, `isPrototypeOf`,
`propertyIsEnumerable`, `toLocaleString`, `__defineGetter__` và ba người anh em. Khoá riêng của object cùng tên vẫn là của
nó (`({ entries: 5 }).entries` là 5); tên không prototype nào có vẫn là `undefined` (`'abc'.map`). `f.caller`,
`f.arguments` là lỗi `TYPE` như JavaScript strict ném. Thời gian, khoảng thời gian, bytes là loại của Kitwork: ngoài các
tên của `Date` trên, chúng không có tên của JavaScript.

## 5. Mô hình xử lý lỗi: `error` và `safe`

`.work` không có `throw`, `try`, `catch`. Báo lỗi bằng `return error(message)`; thu lỗi thành một kết quả bằng
`safe(() => …)`. Hai hàm là builtin (mục 4), không cần import. Quyết định 05/11/2026; `fail` đổi tên thành `error`
07/11/2026. Không có thư viện `kitwork/handle` (chủ dự án chọn 19/11/2026; `CLAUDE.md` 27–28/10/2026 từng dời `safe` và
`fail` sang đó): import nó là lỗi boot nói `error(…)` và `safe(…)` là builtin.

### 5.1. Báo lỗi: `return error(message)`

```js
const parseOrder = (input) => {
  if (!input.items || input.items.length === 0) {
    return error("Đơn hàng phải có ít nhất một món");
  }
  return input;
};
```

- `error(message)` tạo một lỗi mã `FAIL`. Đối số đổi thành chuỗi (`error()` là `"error"`). `message` của lỗi là đúng chữ
  ấy, chỗ gọi nằm riêng ở `at` (mục 5.2): `message` là `"Đơn hàng phải có ít nhất một món"`, `at` là `"router.work:3:12"`.
- **Lỗi đi lên ngay.** Lỗi sinh ra ở đâu (một `error(…)`, một phép toán hỏng, một lời gọi module hỏng) thì lần gọi ấy dừng
  ở đó, các hàm gọi nó cũng dừng, tới `safe` gần nhất hay tới mép của lần chạy. Lỗi không bao giờ là một giá trị cầm được: gán vào biến, bỏ qua giá trị trả về, bọc trong biểu thức
  (`typeof error("x")`) đều không giữ nó lại. Lỗi trong hàm gọi lại của `map` đi lên qua `map` tới `safe` bên ngoài.
- Trong một hàm viết `return error(…)`; `() => error(…)` cũng là một lần trả về. `error(…)` đứng riêng không `return`
  trong một hàm là lỗi biên dịch (`error(…) đứng riêng trong một hàm: viết return error(…)`): nó dừng hàm như `return`,
  dòng ấy phải cho thấy. Cấp cao nhất của file không có `return`: ở đó `error(…)` đứng riêng và dừng lần boot. Một
  chương trình tự khai tên `error` thì đó là hàm của nó.
- Không có `safe` nào bao ngoài thì lỗi tới host ở mép lần chạy. Trên HTTP, lỗi chưa xử lý là 500 bằng chữ trơn; lỗi
  `INVALID_INPUT` (dữ liệu từ ngoài không đọc được: body không phải JSON, `struct.parse` của [`schema.md`](schema.md)) là
  400, vì người gửi phải sửa. Lý do vào log, không vào câu trả lời. Ở desktop, mobile, bridge của trang theo cùng ranh giới.

### 5.2. Thu lỗi thành kết quả: `safe(() => ...)`

```js
import * as router from "kitwork/router";

router.post((context) => {
  const result = safe(() => parseOrder(context.request.json()));
  if (result.error) {
    return context.status(400).json({ message: result.error.message });
  }
  return context.json(result.value);
});
```

`safe(f)` gọi `f` không đối số, một lần, và luôn trả về một object thường với đúng hai khoá, theo thứ tự này:

```js
{ value: kết_quả, error: null }                       // f chạy xong, kể cả khi trả false, 0, "", null, undefined
{ value: undefined, error: { code, message, at } }    // f hỏng
```

- Thành công khi `result.error === null`; đọc `result.error` trước `result.value`. Không có `ok`.
- Object lỗi có `code` (mục 5.4), `message` và `at`, theo thứ tự ấy; thêm `fields` khi lỗi nêu các trường của dữ liệu
  ([`schema.md`](schema.md)), `progress` khi lỗi nói đã làm được bao nhiêu trước khi dừng (`PARTIAL`). Nó là object
  thường, sửa được.
- `message` chỉ là chữ: của `error(…)`, hay của engine, không bao giờ có chỗ ở đầu. `at` là `"file:dòng:cột"` của chỗ hỏng
  thật, frame trong cùng tại lệnh hỏng, kể cả khi nó nằm sâu trong một hàm được gọi từ trong `safe` (lỗi trong `findUser`
  báo dòng của `findUser`, không phải dòng `safe(`); `null` khi không lần chạy nào đặt chỗ cho lỗi (`safe` gọi thẳng một
  hàm của engine: `safe(JSON.parse)`). Log và thân 500 khi phát triển in `at: message`. `at` là chỗ trong mã của app, có cả
  ở production; nó ở lại server (mục 5.3). Lỗi `routine.wait` trả về có cùng hình ([`routine.md`](routine.md)).
- `safe` lồng nhau: kết quả của `safe` bên trong là một giá trị thường.
- `safe` không hoàn tác việc đã làm. Muốn nhiều lần ghi cùng thành hay cùng hỏng, đặt chúng trong `db.transaction(…)`; lỗi
  phải thoát khỏi hàm của giao dịch để rollback.
- **Lỗi biên dịch:** `safe` không đối số, hai đối số trở lên, hay đối số chắc chắn không phải hàm (chữ, lời gọi, object,
  mảng, phép toán): `safe nhận một hàm: viết safe(() => 42)`. Lúc chạy, đối số không phải hàm là lỗi `TYPE`.
  `x.safe()` không đối số là lỗi biên dịch: viết `safe(() => x)`.

### 5.3. `error(...)` và `context.error(...)`

| Cách viết | Trách nhiệm |
|---|---|
| `return error("Không đủ số dư")` | hàm nghiệp vụ báo lỗi, không cần context của route hay màn hình |
| `safe(() => checkout())` | nơi gọi nhận kết quả để tự xử lý |
| `return context.status(403).error("Bạn không có quyền")` | context tạo một câu trả lời lỗi; trên HTTP, status quyết định mã phản hồi |

Lỗi của ngôn ngữ và câu trả lời của context là hai thứ khác nhau: `safe` thu thứ nhất; `context.error(…)` là một câu
trả lời, không thành lỗi nghiệp vụ vì tên của nó. Mặc định 403 trong guard, 500 trong cửa: [`router.md`](router.md).
Trên giao diện, chỉ mã công khai và thông báo phù hợp đi xuống trình duyệt; `at`, chỗ trong code, bí mật, đường dẫn ở lại
server.

### 5.4. Mã lỗi

Mã là một tập đóng (`core/value/failure.go`); mã lạ một module trả về thành `FAIL`. Lỗi kiểu JavaScript mang tên lỗi của
JavaScript: `TYPE` là `TypeError`, `RANGE` là `RangeError` (engine2 từ 02/11/2026; chủ dự án chọn giữ 19/11/2026, thay bản
27/10/2026 ghi gọi thứ không phải hàm là `NOT_A_FUNCTION`, `RangeError` là `INVALID_ARGUMENT`).

| Mã | Khi nào |
|---|---|
| `FAIL` | `error(…)` |
| `TYPE` | sai loại: đọc thành viên của `null`, gọi thứ không phải hàm (`x không phải hàm (là number): không gọi được`), không có method ấy, ghi vào giá trị đóng băng hay giá trị nguyên thuỷ, `for…of` thứ không lặp được, `JSON` gặp vòng |
| `RANGE` | ngoài khoảng: `repeat(-1)`, `toFixed(101)`, chuỗi quá dài, ngày hỏng, thời gian ngoài khoảng, `Array(-1)` |
| `INVALID_ARGUMENT` | đối số sai cho một hàm của engine; khoảng đệm mảng quá lớn; trải quá 65 535 đối số |
| `INVALID_INPUT` | dữ liệu từ ngoài không đọc được: `JSON.parse`, URI hỏng, body |
| `UNSUPPORTED` | engine không làm việc ấy: khoá không phải chỉ số trên mảng |
| `TOO_LARGE` | quá trần của nơi lưu hay của builtin (mảng quá 2^24 phần tử) |
| `NOT_FOUND`, `DENIED`, `CONFLICT`, `PARTIAL`, `TIMEOUT`, `NETWORK`, `DATABASE`, `UNAVAILABLE` | lỗi của module, tả ở tài liệu của module |
| `CANCELLED` | việc của `kitwork/routine` bị huỷ |
| `NOT_INITIALIZED` | một hàm dùng `let`, `const` trước khi dòng khai nó chạy (mục 2.2), `ReferenceError` của JavaScript |

`safe` bắt mọi mã trên. **Không bao giờ bắt** những thứ dừng cả lần chạy (mục 7): hết năng lượng, quá trần byte của lần
chạy hay của host, tràn ngăn xếp gọi, request bị huỷ hay hết giờ (`EXECUTION_CANCELLED`), lỗi nội bộ của engine. Chúng
đi thẳng tới host, kể cả khi bị bọc trong `safe`.

## 6. Chương trình, file và module

### 6.1. File và chương trình

Một file `.work` là một chuỗi câu lệnh (mục 3.4). Mỗi file boot chạy là gốc của một chương trình: compiler gom file ấy
cùng mọi file nó import, trực tiếp hay gián tiếp (`core/compiler/bundler.go`).
- Mỗi file được import chạy **một lần trong một chương trình**, trước thân của file gốc, theo thứ tự import sâu trước;
  hai đường tới cùng một file (`./b.work` và `app/core/b.work`) là một file.
- Mỗi file có tầm tên riêng: tên cấp cao nhất của file này không đụng tên cùng chữ của file khác.
- Vòng import (kể cả file tự import mình) là lỗi: `native bundle: import cycle tại <đường dẫn>`.
- Mỗi file boot chạy là một chương trình riêng, nên một file được nhiều file import thì chạy lại trong từng chương trình
  ấy; lời khai của nó (bảng, job, component) được tìm lại theo chỗ viết, không tính là khai lần hai
  ([`module.md`](module.md)).

### 6.2. Import

```ebnf
ImportDecl     = "import" ( ImportSource
                          | "{" [ ImportSpec { "," ImportSpec } [ "," ] ] "}" "from" ImportSource
                          | "*" "as" identifier "from" ImportSource
                          | identifier "from" ImportSource ) .
ImportSpec     = IdentifierName [ "as" identifier ] .        /* từ khoá ở bên trái cần "as" */
ImportSource   = string_lit .
```

| Nguồn | Viết | Dạng nhận |
|---|---|---|
| Thư viện của Kitwork | `"kitwork/<tên>"`, một chữ sau `kitwork/` | `* as x`, `{ a, b as c }`, `x` (cũng là cả thư viện, không phải `default`) |
| File từ gốc app | `"app/<đường dẫn>"`, đi thẳng từ thư mục của `module.work` xuống | `{ … }`, `x` (`export default`), không tên |
| File tương đối | `"./…"`, `"../…"` | như trên |

- `import * as x` chỉ dùng với thư viện: file thì gọi tên từng thứ cần (`import * chỉ dùng với thư viện của Kitwork
  ("kitwork/<tên>"); file của site thì gọi tên từng thứ cần`).
- `import "kitwork/x"` không tên, `"kitwork"` trần, tên trần khác (`"lodash"`, `"node:fs"`, URL) là lỗi.
- **Không ra ngoài app:** đường tương đối hay tuyệt đối tới một file ngoài gốc app là lỗi biên dịch; `app/` không nhận
  `.`, `..`, đường tuyệt đối; `.database/` là dữ liệu, không import được.
- Import viết đủ tên file, cả đuôi, như module của JavaScript: `"./lib"` không tìm `lib.work` (lỗi biên dịch nói tên
  đúng: `import "./lib": không có file ấy; import viết đủ tên file, cả đuôi: "./lib.work"`). Chỉ file `.work` là module;
  file đuôi khác (`.js`, `.txt`, …) là lỗi biên dịch.
- `import "./thư-mục"` không tên chạy mọi file `.work` ngay trong thư mục ấy, theo thứ tự tên; lấy theo tên từ một thư mục
  là lỗi.
- `import`, `export` chỉ đứng ở cấp cao nhất của file, không trong hàm hay khối (lỗi biên dịch).
- Import một tên mà file kia không export (hay `import x from` một file không có `export default`) là lỗi biên dịch ở
  dòng import: `import { b } from "./lib.work": file ấy không export b`.
- Tên import là hằng (gán là lỗi biên dịch). Dùng tên trước dòng import là lỗi biên dịch như dùng trước dòng khai.
- Import thư viện chạy ở dòng nó đứng; import file chạy trước thân của file gốc, bất kể dòng.
- Không có `import()`, `import.meta`, `with { … }`, `import x, { y }`, `import { "a-b" as c }`.

**Thư viện.** `"kitwork/<tên>"` là một thư viện của engine (`library/<tên>`), mỗi cái một tài liệu:

| Tên | Việc | Tài liệu |
|---|---|---|
| `application` | manifest của host, chỉ trong `application.work` | [`application.md`](application.md) |
| `module` | gốc app, chỉ trong `module.work` | [`module.md`](module.md) |
| `router` | cửa và luật, chỉ trong `router.work`, `config.work` | [`router.md`](router.md), [`view.md`](view.md) |
| `design` | giao diện của một thư mục, chỉ trong `design.work` | [`design.md`](design.md) |
| `component` | component của app | [`component.md`](component.md) |
| `schema` | bảng, cột, struct; chỉ trong file thường | [`schema.md`](schema.md) |
| `database` | `connection()`, câu lệnh | [`database.md`](database.md) |
| `file` | file của app, `file.env` | [`file.md`](file.md) |
| `http` | gọi HTTP ra ngoài | [`http.md`](http.md) |
| `storage` | bộ nhớ, đĩa, datastore theo khoá | [`storage.md`](storage.md) |
| `routine` | việc chạy song song, chờ | [`routine.md`](routine.md) |
| `queue` | việc nền bền | [`queue.md`](queue.md) |
| `schedule` | lịch bền | [`schedule.md`](schedule.md) |
| `napas` | mã VietQR, EMV | [`napas.md`](napas.md) |

`kitwork/tunnel` có tên nhưng chưa phục vụ: import là lỗi `UNSUPPORTED` lúc boot. `kitwork/handle` không phải thư viện
(mục 5): import nó cũng là lỗi `UNSUPPORTED` lúc boot, nói `error(…)` và `safe(…)` là builtin. Một thư viện nằm
sai file là lỗi boot nêu file đúng (`kitwork/router is imported by a.work, a plain file …: only router.work and
config.work import it`).

`kitwork` là một tên toàn cục (mục 4): mỗi thư viện là `kitwork.<tên>`, và `import * as x from "kitwork/x"` là
`const x = kitwork.x`.

### 6.3. Export

```ebnf
ExportDecl     = "export" ( "default" Expression
                          | "const" Binding
                          | FunctionDecl
                          | "{" [ ExportSpec { "," ExportSpec } [ "," ] ] "}" ) .
ExportSpec     = identifier [ "as" IdentifierName ] .
```

- Giá trị được export là giá trị lúc cấp cao nhất của file chạy xong; không có liên kết sống.
- Một file một `export default` (`một file chỉ có một export default: …`). `export { a as default }` cũng là default.
- `export { … }` là các tên ngăn bằng dấu phẩy, mỗi tên khai ở cấp cao nhất của file; tên chưa khai là lỗi biên dịch.
- Không có `export *`, `export … from` (lỗi biên dịch: import tên ấy rồi export nó), `export var`, `export class`.
- Export của file có vai (router.work, config.work, …) bị bỏ qua.

### 6.4. File có vai

Tên file quyết định vai của nó; mọi file `.work` khác là file thường.

| File | Ở đâu | Viết gì |
|---|---|---|
| `application.work` | thư mục chạy host | manifest: `import * as app from "kitwork/application"`, mỗi dòng một `app.<tên>(…)`, `app.run()` cuối ([`application.md`](application.md)) |
| `module.work` | gốc app | một dòng `import * as module from "kitwork/module"`, `module.timezone(…)`, `module.routes(…)`; không import khác ([`module.md`](module.md)) |
| `router.work` | thư mục của một route | `import * as router from "kitwork/router"`; cửa `router.get(…)`, `router.view(…)` và luật ([`router.md`](router.md)) |
| `config.work` | thư mục của một route | luật của thư mục, không cửa ([`router.md`](router.md)) |
| `design.work` | thư mục của một route | `import * as design from "kitwork/design"`, mỗi dòng một lời gọi ([`design.md`](design.md)) |
| file thường | bất kỳ đâu boot đọc | mọi câu lệnh; nơi khai bảng, job, lịch, component (`component.define`) |

Thư viện có tập chữ đóng: chữ lạ là lỗi boot kèm chữ gần nhất (`router has no word gett: did you mean router.get?`).

**Boot đọc gì.** Boot đọc mọi file `.work` của app, trừ thư mục bắt đầu bằng `_` hay `.` (`.well-known` được đọc) và
liên kết mềm. File trong thư mục `_` vẫn import được: đó là chỗ của code chỉ chạy khi có file import nó. `.shared/`,
`.schedule/` ở gốc app là lỗi boot (chỗ cũ, tới 16/11/2026).

**Thứ tự boot** của một app ([`module.md`](module.md)): `module.work`; lượt khai (mọi file thường, theo đường dẫn);
`design.work` rồi `config.work` ở gốc app; lượt nạp việc (lại mọi file thường); rồi từng site, từ thư mục gốc xuống, mỗi
thư mục `design.work`, `config.work`, `router.work` (app không có `module.routes` thì gốc app là site, nên hai file đầu
chạy thêm lần nữa ở đây). Phần cấp cao nhất của mỗi file chạy lúc ấy, với mọi câu lệnh (lời gọi, vòng lặp, `console`), có năng lượng
và trần byte như một request (mục 7); xong thì đóng băng (mục 1.7). Lỗi ở bước nào thì app đóng, các site của nó trả 503,
log nêu file, dòng, cột.

**Lỗi hiện ra ở đâu.**

| Lúc | Dạng |
|---|---|
| biên dịch (cú pháp) | `assemble error: router.work:3:1: …` |
| biên dịch (tên, phạm vi) | `a.work:1:18: nope chưa được khai…` |
| gom file | `native bundle: …` (chưa có dòng) |
| chạy lúc boot | `<app>/<đường dẫn>: <MÃ>: <file>:<dòng>:<cột>: <chữ>` |
| chạy lúc request | 500, log `[site] GET / (/router.work): 500: TYPE: router.work:2:64: …` |

## 7. Giới hạn của một lần chạy

Mỗi lần chạy (một request, một job, phần cấp cao nhất của một file lúc boot) có trần. Bốn dòng đầu dừng cả lần chạy,
`safe` không bắt (mục 5.4); các trần khác là lỗi thường.

| Trần | Giá trị | Quá thì |
|---|---|---|
| Năng lượng | mặc định 10 000 000 mỗi request (cộng mọi lần chạy của request ấy); `app.limits({ energy })` hay bảng tenant đổi được; lệnh của `app.commands` không trần | dừng: `Energy Limit Exceeded: Execution halted` |
| Byte tạo ra | mặc định 64 MiB mỗi lần chạy; pool của host tuỳ chọn | dừng: `Memory Limit Exceeded: Execution halted` (của host: `UNAVAILABLE`) |
| Khung gọi | 64, kể cả cấp cao nhất; hàm gọi lại của builtin và hàm của `safe` cũng là một khung | dừng: `Stack overflow: Call stack limit exceeded` |
| Request bị huỷ, hết giờ | kiểm mỗi 64 lệnh | dừng: `EXECUTION_CANCELLED` |
| Độ dài chuỗi | 536 870 888 byte UTF-8 | `RANGE` (`invalid string length`) |
| Đối số viết trong một lời gọi | 255 | lỗi biên dịch |
| Đối số trải vào một lời gọi | 65 535 | `INVALID_ARGUMENT` |
| Khoảng đệm khi ghi quá cuối mảng | 1 048 576 phần tử | `INVALID_ARGUMENT` |
| Mảng một builtin tạo | 2^24 phần tử | `TOO_LARGE` |
| Độ lồng của `JSON.parse` | 513 | `INVALID_INPUT` |
| Độ lồng của mã (ngoặc, khối, hàm, toán tử, `a.b.c`) | 1 000 | lỗi biên dịch (`mã lồng quá sâu …`) |
| Tầng hàm một biến với tới | 255 | lỗi biên dịch |
| Mã của một chương trình | 2 MiB bytecode | lỗi biên dịch |

**Năng lượng** là giá của từng lệnh VM (`core/runtime/instruction.go`): đọc, ghi biến 2; cộng, trừ 2; nhân 6; chia, chia
dư 25; đọc thành viên 12; ghi 10; tạo object, mảng 80; gọi hàm 150. Builtin và module tính thêm theo việc chúng làm
(byte chữ đọc, byte tạo ra, phần tử). Năng lượng là thứ chặn vòng lặp không dừng, đệ quy và việc quá lớn; nó không đo
thời gian.

**Byte** là những gì lần chạy tạo ra (chuỗi, mảng, object, khung gọi), tính trước khi tạo: một lời gọi tạo quá trần dừng
trước khi kịp tạo.

**Dựng chuỗi bằng `+`** (19/11/2026, `core/value/join.go`): `s = s + phần`, `s += phần` và `` s = `${s}…` `` ghi phần mới
nối tiếp sau `s` khi `s` là chuỗi lần chạy vừa dựng bằng chính cách ấy, nên một chuỗi dựng từng mảnh tốn theo độ dài của
nó, không theo bình phương: 100 000 lần `s += "x"` chạy trong vài mili giây, trong trần năng lượng của một request (trước
đó 30 000 lần, một chuỗi 30 KB, đã hết năng lượng). Mỗi chuỗi tạo ra trên đường đi giữ nguyên giá trị của nó, như
JavaScript. Chuỗi được dựng có chỗ trống phía sau tới bằng độ dài của nó, tính vào byte tạo ra; `s.concat(…)` vẫn chép cả
hai vế mỗi lần.

Nghĩa ở host (giới hạn request đồng thời của site, pool của host, bảng tenant): [`application.md`](application.md),
[`module.md`](module.md).

## 8. Chỗ engine chưa giữ luật

Luật của `.work` là nghĩa của JavaScript hoặc lỗi biên dịch, không bao giờ một giá trị khác. Rà ngày 18/11/2026 (đọc code
và chạy từng đoạn qua `CompileFile`, đường của file thật) và rà lần hai ngày 19/11/2026 (corpus 4 093 ca so với node, thêm
510 ca mới về câu lệnh, mẫu tách, định dạng số, `replace`, ngày UTC, JSON; mỗi chỗ lệch chạy lại qua `CompileFile`) thấy
những chỗ dưới đây engine2 chưa giữ luật. Mỗi chỗ là một
việc của engine, không phải nghĩa của ngôn ngữ: viết code đừng dựa vào chúng.

### 8.1. Cùng đoạn mã, khác giá trị: lỗi của parser và compiler

Không còn chỗ nào được biết: các chỗ rà ngày 18/11/2026 thấy đã theo JavaScript hay thành lỗi biên dịch trong cùng ngày
(`function` trong `case`, hàm biểu thức có tên, mặc định của tham số và `length`, mẫu mảng theo phép lặp, dùng `let`,
`const` trước khi có).

### 8.2. Cùng đoạn mã, khác giá trị: loại giá trị và thư viện

Không còn chỗ nào được biết: các chỗ rà ngày 18/11/2026 (đến từ thiết kế riêng của Kitwork, không prototype, giá trị
thời gian, hay thư viện chưa đủ) đã theo JavaScript hay thành lỗi trong cùng ngày (`Date(…)` không `new`, so bằng hai thời
gian, `constructor` và `__proto__` viết thẳng, method đọc ra khỏi giá trị, chữ của một hàm, chữ của một thời gian). Các chỗ
rà lần hai ngày 19/11/2026 cũng vậy, cùng ngày (`Array(n)` và chỗ trống, method của JavaScript mà Kitwork chưa có,
`Object(5)`, `Math` và `JSON` làm dữ liệu, `parseInt` ở cơ số lạ quá 2^53; `sort` với hàm so không nhất quán ghi ở mục
4.3).

### 8.3. `.work` nhận, JavaScript từ chối

Các chỗ rà ngày 18/11/2026 đã thành lỗi biên dịch trong cùng ngày (hai câu lệnh trên một dòng, khối chưa đóng, khoá object
là biểu thức, mũi tên làm toán hạng, `if` làm giá trị, `return` ở cấp file, `import`/`export` lồng, tên strict giữ riêng,
danh sách export). Rà lần hai (19/11/2026) thấy `f.caller`, `f.arguments` đọc ra `undefined` thay vì ném như JavaScript
strict; đã thành lỗi `TYPE` cùng ngày (mục 4.4). Không còn chỗ nào được biết.

### 8.4. JavaScript nhận, `.work` từ chối

Phần lớn là tập con có chủ đích, đã nói ở từng mục (3.2–3.6, 4.4, 6.2). Ngoài ra:
- lúc chạy: gán `a.length`, khoá không phải chỉ số trên mảng (`UNSUPPORTED`), ghi vào hàm, vào thời gian, khoảng thời
  gian, bytes (`TYPE`; JavaScript ghi được khoá của `Date`, phần tử của `Uint8Array`), `delete a[i]` của một
  phần tử mảng (`UNSUPPORTED`), `delete f.length`, `delete f.name` (`TYPE`);
- lúc biên dịch: `delete` một thứ không phải khoá (`delete 5`), `delete o?.k`, `Date(…)` không `new`;
- `===`, `==` giữa hai thời gian (lỗi `TYPE` lúc chạy, mục 4.3);
- `s[k]()` với `k` (một biến) là tên method của giá trị `s` (lỗi `TYPE`, mục 4.2): method chỉ chạy khi gọi thẳng trên giá trị;
- đổi một hàm thành chuỗi (lỗi `TYPE`, mục 1.4): JavaScript dùng mã nguồn của hàm;
- đổi một thời gian thành chuỗi (lỗi `TYPE`, mục 1.4, 4.3): JavaScript viết theo múi giờ của máy chạy; `t == "x"`,
  `t == 0` cũng là lỗi ấy (JavaScript so chữ của nó);
- `new Date("hello")`: lỗi `RANGE`, không phải Invalid Date (mục 4.3);
- method JavaScript chưa có (mục 4.4, lỗi `TYPE` lúc chạy): `match`, `search`, `localeCompare`, `normalize`,
  `toLocaleString`, `keys`/`values`/`entries` của mảng, `getFullYear` và các getter, setter của ngày, `call`, `apply`,
  `bind`;
- tên tĩnh của builtin mà JavaScript có, Kitwork chưa có (mục 4.4, lỗi `TYPE` lúc chạy): `Array.prototype`,
  `Object.create`, `String.raw`, `console.table`, …;
- dấu phẩy cuối trong mảng, lời gọi, danh sách tham số, mẫu mảng của phép gán (mẫu của khai báo thì nhận);
- `const undefined = 1`;
- `#!` ở đầu file, BOM, CR đứng riêng làm dòng mới, khoảng trắng Unicode: lỗi biên dịch nói rõ, chưa đọc theo
  JavaScript; mở thứ nào là việc chủ dự án duyệt (luật 27/10/2026; `/* … */` mở 19/11/2026, mục 3.2).

### 8.5. Lỗi nội bộ và thông báo cũ

- Lỗi riêng của `fail(…)` (`fail(…) đã đổi tên thành error(…)…`) không tới được file thật: luật tên chưa khai báo trước;
  chỉ đoạn mã của test (`CompileSource`) thấy nó.

## 9. Đã chốt, engine chưa làm

Quyết định của chủ dự án mà engine2 chưa theo; lý do và lịch sử ở `docs/KITWORK_LANG.md` mục 3, 6 của repo gốc. Khi làm,
mục tương ứng của tài liệu này đổi cùng commit.

- **Cách viết JavaScript còn thiếu** (27/10/2026, câu 1): `Set` (`Object.groupBy` làm 19/11/2026, mục 4.3).
- **Kiểm `null` lúc biên dịch, lát 2 và 3** (27/10/2026; lát 1 làm 19/11/2026, mục 3.7): theo giá trị có thể `null` qua
  `return` của một hàm trong cùng file, rồi qua `export` sang file khác (handle database thường đi qua `export`).
- **Bản ghi lần chạy của queue** (`kitwork_runs`) chưa có cột cho `at`: `message` trong lỗi của bản ghi vẫn mở đầu bằng chỗ
  (`"core/mail.work:4:12: …"`), như dòng log của nó; tách ra là thêm một cột vào bảng của queue (19/11/2026, khi `at` tách
  khỏi `message` của `safe` và `routine.wait`).

Hai câu từng mở đã được chọn 19/11/2026: mã của lỗi kiểu JavaScript giữ `TYPE` và `RANGE` (mục 5.4); không có
`kitwork/handle`, `error` và `safe` là builtin (mục 5).

## 10. Ghi chú hiện thực

- **Chỗ của biến** (`core/compiler/slots.go`): tham số và biến cục bộ của hàm có chỉ số chỗ lúc biên dịch (`LOAD_SLOT`,
  `STORE_SLOT`); biến của hàm bao ngoài đọc qua số tầng khung (`LOAD_OUTER`, `STORE_OUTER`); builtin đọc từ chỗ của nó
  (`BUILTIN n`); tên cấp cao nhất của file đọc, ghi theo tên (`LOAD`, `STORE`).
- **Hằng gập lúc biên dịch:** `Math.PI`, `NaN`, `Infinity` và các hằng số khác của builtin.
- **Đóng băng** (mục 1.7): `vm.Freeze`, `core/runtime/freeze.go`.
- **Trọng tài:** `test/compiler/parity_test.go` so từng đoạn với node; biểu thức `.ui` có test ba chiều (JavaScript
  thật, VM, JS sinh cho trình duyệt). Một thay đổi nghĩa phải giữ hay tăng số ca khớp.

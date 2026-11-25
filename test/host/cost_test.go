package host_test

import (
	"net/http/httptest"
	"runtime"
	"testing"

	"github.com/huynhnhanquoc/engine/host"
)

// Giá của một request (25/11/2026, "giảm giá chuỗi host"): file tĩnh của site giữ ấm qua chuỗi của bản thiết kế (năm
// guard, ba câu KitDB, routes, memory, energy, rồi finally viết một dòng console) có ngân sách lần cấp phát và byte làm
// ra, không tính request httptest dựng (dùng lại một request): 62 lần, khoảng 4 KB; trước 25/11 (HEAD 1f58c29b) 119
// lần, 15,9 KB. Phần lớn nhờ máy của lượt chạy dùng lại (runtime.Take: ngăn xếp 8 KB), rồi SQL nhớ theo dạng câu, đối số
// của m.Call không cấp phát, header của file giữ ấm dùng chung.
func TestAWarmFileCostsLittle(t *testing.T) {
	if raced {
		t.Skip("-race: the pool drops machines on purpose and the detector allocates; the budget is measured without it")
	}
	w, console, _ := blueprintDoor(t)
	allocs, bytes := requestCost(t, w, console, "https://shop.vn/assets/app.css")
	t.Logf("%.0f allocations, %d bytes a request", allocs, bytes)
	if allocs > 65 {
		t.Errorf("%.0f allocations a request, the budget is 65", allocs)
	}
	if bytes > 5<<10 {
		t.Errorf("%d bytes a request, the budget is 5 KB", bytes)
	}
}

// Một trang qua cùng chuỗi ấy đi thêm một đoạn file tĩnh không đi: lượt chạy của site (serveSite), trên một máy lấy từ
// kho như lượt của host. Ngân sách của file tĩnh không thấy đoạn này: site dựng máy mới mỗi request thì nó vẫn xanh.
// Hai trang của bản thiết kế: cửa get của shop.vn trả chữ; don-hang đọc hai dòng orders của tenant rồi trả JSON.
func TestAPageCostsLittle(t *testing.T) {
	if raced {
		t.Skip("-race: the pool drops machines on purpose and the detector allocates; the budget is measured without it")
	}
	w, console, _ := blueprintDoor(t)
	for _, c := range []struct {
		target string
		allocs float64
		bytes  uint64
	}{
		{"https://shop.vn/", pageAllocs, pageBytes},
		{"https://shop.vn/don-hang", ordersAllocs, ordersBytes},
	} {
		allocs, bytes := requestCost(t, w, console, c.target)
		t.Logf("%s: %.0f allocations, %d bytes a request", c.target, allocs, bytes)
		if allocs > c.allocs {
			t.Errorf("%s: %.0f allocations a request, the budget is %.0f", c.target, allocs, c.allocs)
		}
		if bytes > c.bytes {
			t.Errorf("%s: %d bytes a request, the budget is %d", c.target, bytes, c.bytes)
		}
	}
}

// Ngân sách của hai trang, trên số đo được ngày 25/11/2026: cửa get 60 lần, 3,8 KB; don-hang 88 lần, 6,0 KB. Một máy
// mới cho lượt của site là thêm hai lần cấp phát và hơn 8 KB (ngăn xếp của nó): ngân sách byte bắt được.
const (
	pageAllocs   = 63
	pageBytes    = 5 << 10
	ordersAllocs = 92
	ordersBytes  = 7 << 10
)

// requestCost là số lần cấp phát và số byte làm ra của một request GET tới target (User-Agent k6, như các guard của bản
// thiết kế đòi), trả 200, không tính request httptest dựng: một request dùng lại cho mọi lần đo. Lần đầu không đo: site
// biên dịch, file vào RAM, SQL được nhớ.
func requestCost(t *testing.T, w *host.Website, console *lines, target string) (allocs float64, bytes uint64) {
	t.Helper()
	r := httptest.NewRequest("GET", target, nil)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("User-Agent", "k6")
	one := func() {
		rec := httptest.NewRecorder()
		w.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatal(rec.Code, rec.Body)
		}
		console.take()
	}
	one()
	allocs = testing.AllocsPerRun(200, one)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 200 {
		one()
	}
	runtime.ReadMemStats(&after)
	return allocs, (after.TotalAlloc - before.TotalAlloc) / 200
}

package host_test

import (
	"net/http/httptest"
	"runtime"
	"testing"
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
	r := httptest.NewRequest("GET", "https://shop.vn/assets/app.css", nil)
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
	one() // site biên dịch, file vào RAM, SQL được nhớ
	allocs := testing.AllocsPerRun(200, one)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 200 {
		one()
	}
	runtime.ReadMemStats(&after)
	bytes := (after.TotalAlloc - before.TotalAlloc) / 200
	t.Logf("%.0f allocations, %d bytes a request", allocs, bytes)
	if allocs > 65 {
		t.Errorf("%.0f allocations a request, the budget is 65", allocs)
	}
	if bytes > 5<<10 {
		t.Errorf("%d bytes a request, the budget is 5 KB", bytes)
	}
}

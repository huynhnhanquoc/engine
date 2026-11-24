package host_test

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Chuỗi của host cho một request (24/11/2026): năm guard của bản thiết kế docs/host/blueprint, ba guard đọc KitDB
// (host/system.work, lát 3; trước đó là dữ liệu giả lập trong bộ nhớ), qua ServeHTTP với một httptest.ResponseRecorder,
// rồi site, rồi error hay finally (console vào một buffer). Spec mục 12 đo engine2 21/11 cho chuỗi bốn guard, bảng trong
// bộ nhớ: 7,66 µs, 45 lần cấp phát (một luồng).

func benchmarkChain(b *testing.B, target string, headers ...string) {
	w, console, _ := blueprintDoor(b)
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("GET", target, nil)
		r.RemoteAddr = "203.0.113.7:5555"
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		w.ServeHTTP(httptest.NewRecorder(), r)
		console.take()
	}
}

// Qua cả năm guard, routes, memory, energy, rồi cửa get của site apps/03dt/shop.vn (200), rồi finally.
func BenchmarkHostChainThrough(b *testing.B) {
	benchmarkChain(b, "https://shop.vn/", "User-Agent", "k6")
}

// Guard đầu chuyển sang https.
func BenchmarkHostChainRedirect(b *testing.B) {
	benchmarkChain(b, "http://shop.vn/", "User-Agent", "k6")
}

// Guard thứ tư từ chối (thiếu User-Agent).
func BenchmarkHostChainRefused(b *testing.B) { benchmarkChain(b, "https://shop.vn/") }

// Trang don-hang: năm guard, rồi một câu đọc orders của tenant (hai dòng), rồi JSON.
func BenchmarkHostChainKitDB(b *testing.B) {
	benchmarkChain(b, "https://shop.vn/don-hang", "User-Agent", "k6")
}

// Mốc: chỉ httptest, không guard nào.
func BenchmarkHostChainNothing(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("GET", "https://shop.vn/", nil)
		rec := httptest.NewRecorder()
		rec.WriteHeader(501)
		_ = r
	}
}

// File tĩnh (lát 4): năm guard, routes, memory, energy, rồi /assets/app.css của shop.vn, mà warm của bản thiết kế giữ
// trong RAM; rồi finally. Spec mục 12: "file tĩnh, site giữ ấm" của engine2 là 4,78 µs một luồng, 2,58 µs mười sáu luồng
// (21/11), không qua guard nào; thiết kế mới, có cache truy vấn, ước 10–11 µs một luồng.
func BenchmarkHostChainStatic(b *testing.B) {
	benchmarkChain(b, "https://shop.vn/assets/app.css", "User-Agent", "k6")
}

// File tĩnh của site không giữ ấm: warm(false), mỗi request mở file trên đĩa.
func BenchmarkHostChainStaticCold(b *testing.B) {
	folder := blueprintFolder(b)
	source, err := os.ReadFile(filepath.Join(folder, "application.work"))
	if err != nil {
		b.Fatal(err)
	}
	cold := strings.Replace(string(source), `.warm((context) => context.site.hostname === "shop.vn")`, ".warm(false)", 1)
	if cold == string(source) {
		b.Fatal("the blueprint has no warm line to change")
	}
	w, console, _ := door(b, folder, cold)
	b.ReportAllocs()
	for b.Loop() {
		r := httptest.NewRequest("GET", "https://shop.vn/assets/app.css", nil)
		r.RemoteAddr = "203.0.113.7:5555"
		r.Header.Set("User-Agent", "k6")
		w.ServeHTTP(httptest.NewRecorder(), r)
		console.take()
	}
}

// BenchmarkHostChainStatic từ mười sáu luồng (GOMAXPROCS): thời gian chia cho số request, như cột 16 luồng của mục 12.
func BenchmarkHostChainStaticParallel(b *testing.B) {
	w, _, _ := blueprintDoor(b)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			r := httptest.NewRequest("GET", "https://shop.vn/assets/app.css", nil)
			r.RemoteAddr = "203.0.113.7:5555"
			r.Header.Set("User-Agent", "k6")
			rec := httptest.NewRecorder()
			w.ServeHTTP(rec, r)
			if rec.Code != 200 {
				b.Errorf("%d %s", rec.Code, rec.Body)
				return
			}
		}
	})
}

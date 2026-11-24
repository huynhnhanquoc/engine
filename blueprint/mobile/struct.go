package mobile

import "github.com/huynhnhanquoc/engine/core/value"

// Mobile là app điện thoại, đóng gói một site; trường lấy từ manifest mobile hôm nay (native/README.md), bỏ version.
// Không có ssl, guards, warm.
type Mobile struct {
	// --- Định danh ---
	id          string // bắt buộc: "org.example.pocketnotes"
	title       string // tên hiện dưới biểu tượng
	icon        string // file biểu tượng, tính từ thư mục manifest
	versionName string // "": "0.1.0"
	versionCode int    // 0: 1

	// --- Lúc dựng ---
	routes value.Value // string | () => string; thư mục site đóng gói vào app, gọi lúc dựng
	start  string      // trang đầu tiên; "": "/"
	links  string      // scheme mở app từ liên kết: "pocketnotes"

	// --- Giao diện ---
	orientation string // "auto" | "portrait" | "landscape"; "": "auto"
	theme       Theme

	// --- Quyền ---
	permissions []string // capability: "secureStorage.get", "notifications.show", …

	// --- Mỗi lượt chạy (như website, context: website.RequestContext) ---
	memory value.Value
	energy value.Value

	// --- Sau khi đã gửi (như website) ---
	success []value.Value
	errors  []value.Value
	finally []value.Value
}

// Theme là chế độ sáng tối và màu của app.
type Theme struct {
	mode  string // "system" | "light" | "dark"
	color string // "#RRGGBB"
}

package desktop

import "github.com/huynhnhanquoc/engine/core/value"

// Desktop là cửa sổ trên máy tính, một app; trường lấy từ native/appfolder/config.go hôm nay. Không có ssl, guards,
// warm: trang chạy trong webview của chính app.
type Desktop struct {
	// --- Lúc mở ---
	title  string      // tên cửa sổ
	icon   string      // file biểu tượng, tính từ thư mục manifest
	routes value.Value // string | () => string; thư mục site hiện trong cửa sổ, gọi một lần lúc mở
	start  string      // trang đầu tiên; "": "/"

	// --- Cửa sổ ---
	window     Window
	chrome     string      // khung cửa sổ: của hệ điều hành hay tự vẽ
	material   string      // nền trong suốt kiểu Windows 11 ("mica", "acrylic"); "": không
	background value.Value // màu nền lúc chưa vẽ xong
	color      value.Value // màu khung
	position   value.Value // vị trí cửa sổ lúc mở
	splash     value.Value // màn chờ lúc mở

	// --- Quyền ---
	permissions []string // capability cấp cho trang: "clipboard.writeText", "shell.open", …

	// --- Mỗi lượt chạy (như website, context: website.RequestContext) ---
	memory value.Value
	energy value.Value

	// --- Sau khi đã gửi (như website) ---
	success []value.Value
	errors  []value.Value
	finally []value.Value
}

// Window là kích thước cửa sổ, điểm ảnh logic.
type Window struct {
	width, height       int
	minWidth, minHeight int
	maximized           bool
}

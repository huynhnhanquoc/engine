package ui_test

// The warnings of the first engine, moved on 06/11/2026: each case is a page and the warnings the first engine
// gave it, word for word and in order (its pairs short of contrast aside: the contrast check has not moved). The
// cases were drawn by both engines side by side; engine2 must give each list as it is.

var warningCases = []struct {
	name  string
	files map[string]string // by path under the site
	drive bool              // the site moves without a reload
	want  []string
}{
	{
		name: "clean",
		files: map[string]string{
			"content.ui": "<content><text>Xin chào</text></content>",
		},
	},
	{
		name: "tag with capitals",
		files: map[string]string{
			"content.ui": "<content><Text>Xin</Text></content>",
		},
		want: []string{
			"content.ui:1:10: tên thẻ <Text> viết thường: <text> (tên thẻ không phân biệt hoa thường, nên đây là <text>)",
		},
	},
	{
		name: "bare required",
		files: map[string]string{
			"content.ui": "<content><form><field><label>Tên</label><input name=\"q\" required /></field></form></content>",
		},
		want: []string{
			"content.ui:1:57: thuộc tính đúng/sai luôn có giá trị: viết required=\"true\" (viết trơn đọc là true)",
		},
	},
	{
		name: "bare italic",
		files: map[string]string{
			"content.ui": "<content><text italic>Xin</text></content>",
		},
		want: []string{
			"content.ui:1:16: thuộc tính đúng/sai luôn có giá trị: viết italic=\"true\" (viết trơn đọc là true)",
		},
	},
	{
		name: "bare exact",
		files: map[string]string{
			"content.ui": "<content><link value=\"/\" exact>Trang chủ</link></content>",
		},
		want: []string{
			"content.ui:1:26: thuộc tính đúng/sai luôn có giá trị: viết exact=\"true\" (viết trơn đọc là true)",
		},
	},
	{
		name: "bare new-tab",
		files: map[string]string{
			"content.ui": "<content><link value=\"https://example.com\" new-tab>Ví dụ</link></content>",
		},
		want: []string{
			"content.ui:1:44: thuộc tính đúng/sai luôn có giá trị: viết new-tab=\"true\" (viết trơn đọc là true)",
		},
	},
	{
		name: "bare strikethrough",
		files: map[string]string{
			"content.ui": "<content><text strikethrough>cũ</text></content>",
		},
		want: []string{
			"content.ui:1:16: thuộc tính đúng/sai luôn có giá trị: viết strikethrough=\"true\" (viết trơn đọc là true)",
		},
	},
	{
		name: "wrapped if",
		files: map[string]string{
			"content.ui": "<content><text if=\"{{ true }}\">Xin</text></content>",
		},
		want: []string{
			"content.ui:1:16: if đã là biểu thức, không bọc {{ }}: viết if=\"true\"",
		},
	},
	{
		name: "wrapped show",
		files: map[string]string{
			"content.ui": "<content><text show=\"{{ 1 > 0 }}\">Xin</text></content>",
		},
		want: []string{
			"content.ui:1:16: show đã là biểu thức, không bọc {{ }}: viết show=\"1 > 0\"",
		},
	},
	{
		name: "wrapped event",
		files: map[string]string{
			"content.ui": "<content><dropdown><button click=\"{{ toggle() }}\">Mở</button><popover><text>a</text></popover></dropdown></content>",
		},
		want: []string{
			"content.ui:1:28: click đã là biểu thức, không bọc {{ }}: viết click=\"toggle()\"",
		},
	},
	{
		name: "show true literal",
		files: map[string]string{
			"content.ui": "<content><text show=\"true\">Xin</text></content>",
		},
	},
	{
		name: "button with only an icon",
		files: map[string]string{
			"content.ui": "<content><button><icon value=\"x\"></icon></button></content>",
		},
		want: []string{
			"content.ui:1:10: <button> chỉ có icon nên chưa có tên để trình đọc màn hình đọc: viết alt trên icon (vd. <icon value=\"x\" alt=\"Đóng\" />)",
		},
	},
	{
		name: "button icon empty alt",
		files: map[string]string{
			"content.ui": "<content><button><icon value=\"x\" alt=\"\"></icon></button></content>",
		},
		want: []string{
			"content.ui:1:10: <button> chỉ có icon nên chưa có tên để trình đọc màn hình đọc: viết alt trên icon (vd. <icon value=\"x\" alt=\"Đóng\" />)",
		},
	},
	{
		name: "button icon with alt",
		files: map[string]string{
			"content.ui": "<content><button><icon value=\"x\" alt=\"Đóng\"></icon></button></content>",
		},
	},
	{
		name: "link with only an image",
		files: map[string]string{
			"content.ui": "<content><link value=\"/\"><image value=\"/logo.svg\" alt=\"\" /></link></content>",
		},
		want: []string{
			"content.ui:1:10: <link> chỉ có ảnh nên chưa có tên để trình đọc màn hình đọc: viết alt trên ảnh, nói nó dẫn tới đâu (vd. <image value=\"/logo.svg\" alt=\"Trang chủ\" />)",
		},
	},
	{
		name: "empty button",
		files: map[string]string{
			"content.ui": "<content><button click=\"go()\"></button></content>",
		},
		want: []string{
			"content.ui:1:10: <button> cần tên đọc được: chữ bên trong, hoặc icon có alt",
		},
	},
	{
		name: "button with a piece",
		files: map[string]string{
			"content.ui":    "<content><button><shop-label /></button></content>",
			"shop-label.ui": "<text>Mua</text>",
		},
	},
	{
		name: "button text inside box",
		files: map[string]string{
			"content.ui": "<content><button><box><text>Mua</text></box></button></content>",
		},
	},
	{
		name: "button empty text",
		files: map[string]string{
			"content.ui": "<content><button><text></text></button></content>",
		},
		want: []string{
			"content.ui:1:10: <button> cần tên đọc được: chữ bên trong, hoặc icon có alt",
		},
	},
	{
		name: "badge with only an icon",
		files: map[string]string{
			"content.ui": "<content><badge color=\"success\"><icon value=\"point-fill\"></icon></badge></content>",
		},
		want: []string{
			"content.ui:1:10: <badge> chỉ có icon cần alt nói nó là gì (vd. <badge alt=\"Đang online\">)",
		},
	},
	{
		name: "badge with only an image",
		files: map[string]string{
			"content.ui": "<content><badge><image value=\"/a.png\" alt=\"\" /></badge></content>",
		},
		want: []string{
			"content.ui:1:10: <badge> chỉ có ảnh cần alt nói nó là gì (vd. <badge alt=\"Đang online\">)",
		},
	},
	{
		name: "empty badge",
		files: map[string]string{
			"content.ui": "<content><badge></badge></content>",
		},
		want: []string{
			"content.ui:1:10: <badge> cần tên đọc được: chữ bên trong, hoặc alt nói nó là gì (vd. <badge alt=\"Đang online\">)",
		},
	},
	{
		name: "badge with alt",
		files: map[string]string{
			"content.ui": "<content><badge alt=\"Đang online\" color=\"success\"><icon value=\"point-fill\"></icon></badge></content>",
		},
	},
	{
		name: "badge with words",
		files: map[string]string{
			"content.ui": "<content><badge>Mới</badge></content>",
		},
	},
	{
		name: "image without alt",
		files: map[string]string{
			"content.ui": "<content><image value=\"/a.png\" /></content>",
		},
		want: []string{
			"content.ui:1:10: image cần alt (alt=\"\" nếu chỉ để trang trí)",
		},
	},
	{
		name: "image with empty alt",
		files: map[string]string{
			"content.ui": "<content><image value=\"/a.png\" alt=\"\" /></content>",
		},
	},
	{
		name: "id with capitals",
		files: map[string]string{
			"content.ui": "<content><section id=\"Quick-Start\"><text>a</text></section></content>",
		},
		want: []string{
			"content.ui:1:19: id \"Quick-Start\" có chữ hoa: id viết bằng chữ thường, số và dấu gạch nối (id=\"quick-start\"); link phải viết đúng hoa thường như id",
		},
	},
	{
		name: "id lowercase",
		files: map[string]string{
			"content.ui": "<content><section id=\"quick-start\"><text>a</text></section></content>",
		},
	},
	{
		name: "prefix out of order",
		files: map[string]string{
			"content.ui": "<content><box hover+large:background=\"paper\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:15: thứ tự tiền tố là đích → độ rộng → trạng thái (hover+large): viết large+hover:background",
		},
	},
	{
		name: "widths twice",
		files: map[string]string{
			"content.ui": "<content><box small,small:padding=\"4\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:15: small,small:padding — độ rộng small viết lặp lại: viết small:padding",
		},
	},
	{
		name: "widths covered",
		files: map[string]string{
			"content.ui": "<content><box small,large:padding=\"4\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:15: small,large:padding — độ rộng là từ đó trở lên, nên small đã gồm cả large: viết small:padding",
		},
	},
	{
		name: "widths covered with a state",
		files: map[string]string{
			"content.ui": "<content><box medium,huge+hover:background=\"paper\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:15: medium,huge+hover:background — độ rộng là từ đó trở lên, nên medium đã gồm cả huge: viết medium+hover:background",
		},
	},
	{
		name: "columns 13",
		files: map[string]string{
			"content.ui": "<content><grid columns=\"13\"><text>a</text></grid></content>",
		},
		want: []string{
			"content.ui:1:16: columns là số cột, số nguyên từ 1 đến 12; \"13\" ở ngoài khoảng đó",
		},
	},
	{
		name: "padding 17px",
		files: map[string]string{
			"content.ui": "<content><box padding=\"17px\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:15: padding=\"17px\" không có trong thang khoảng cách; gần nhất là padding=\"4\" (1rem). Thang: 0, 0.5, 1, 1.5, 2, 2.5, 3, 3.5, 4, 5, 6, 7, 8, 9, 10, 11, 12, 14, 16, 20, 24, 28, 32, 36, 40, 44, 48, 52, 56, 60, 64, 72, 80, 96 (bội của 0.25rem: 4 là 1rem)",
		},
	},
	{
		name: "pin on text",
		files: map[string]string{
			"content.ui": "<content><text pin=\"top\">a</text></content>",
		},
		want: []string{
			"content.ui:1:16: pin ghim một khung vào mép màn hình khi cuộn (box, section, card, list, header, footer…); <text> không ghim được (trang vẫn ghim nó như đã viết)",
		},
	},
	{
		name: "pin on a box",
		files: map[string]string{
			"content.ui": "<content><box pin=\"top\"><text>a</text></box></content>",
		},
	},
	{
		name: "pin beside place",
		files: map[string]string{
			"content.ui": "<content><box place=\"top-right\" pin=\"top\"><text>a</text></box></content>",
		},
		want: []string{
			"content.ui:1:33: pin và place không đi cùng nhau: phần tử có place đã nổi trên cha, không nằm trong dòng chảy để ghim",
		},
	},
	{
		name: "material on text",
		files: map[string]string{
			"content.ui": "<content><text material=\"glass\">a</text></content>",
		},
		want: []string{
			"content.ui:1:16: material là chất liệu của một khung (box, card, section, header, footer, popover…); <text> không mang material được (trang vẫn vẽ material như đã viết)",
		},
	},
	{
		name: "material on view",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view material=\"glass\"><content /></view>",
		},
		want: []string{
			"view.ui:1:7: material là chất liệu của một khung (box, card, section, header, footer, popover…); <view> là nền của cả trang, phía sau không có gì để nhìn qua: đặt material trên một vùng hay một khung bên trong (trang vẫn vẽ material như đã viết)",
		},
	},
	{
		name: "material on a card",
		files: map[string]string{
			"content.ui": "<content><card material=\"glass\"><text>a</text></card></content>",
		},
	},
	{
		name: "lazy on content",
		files: map[string]string{
			"content.ui": "<content lazy=\"true\"><text>a</text></content>",
		},
		want: []string{
			"content.ui:1:10: lazy trên <content>: vùng nội dung luôn có phần ở trên màn hình, trình duyệt không bao giờ bỏ qua nó; đặt lazy trên từng phần ở xa màn hình bên trong (section, grid, card…) (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "lazy on an image",
		files: map[string]string{
			"content.ui": "<content><image value=\"/a.png\" alt=\"a\" lazy=\"true\" /></content>",
		},
		want: []string{
			"content.ui:1:40: lazy bỏ qua một khung ở xa màn hình (section, box, card, grid, list, footer…); ảnh đã tự tải khi tới gần màn hình (loading=\"lazy\") (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "lazy on text",
		files: map[string]string{
			"content.ui": "<content><text lazy=\"true\">a</text></content>",
		},
		want: []string{
			"content.ui:1:16: lazy bỏ qua một khung ở xa màn hình (section, box, card, grid, list, footer…); <text> không phải khung (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "lazy on a section",
		files: map[string]string{
			"content.ui": "<content><section lazy=\"true\"><text>a</text></section></content>",
		},
	},
	{
		name: "wrap false on a paragraph",
		files: map[string]string{
			"content.ui": "<content><text wrap=\"false\">a</text></content>",
		},
		want: []string{
			"content.ui:1:16: wrap=\"false\" giữ một cụm chữ trong câu liền nhau: đặt trên <text> nằm trong một <text> khác (đoạn văn hay tiêu đề đã đứng riêng dòng nên ở đây không có gì để giữ liền)",
		},
	},
	{
		name: "wrap false inside text",
		files: map[string]string{
			"content.ui": "<content><text>a <text wrap=\"false\">b c</text></text></content>",
		},
	},
	{
		name: "wrap false on a heading",
		files: map[string]string{
			"content.ui": "<content><text heading=\"2\" wrap=\"false\">a</text></content>",
		},
		want: []string{
			"content.ui:1:28: wrap=\"false\" giữ một cụm chữ trong câu liền nhau: đặt trên <text> nằm trong một <text> khác (đoạn văn hay tiêu đề đã đứng riêng dòng nên ở đây không có gì để giữ liền)",
		},
	},
	{
		name: "popover show true",
		files: map[string]string{
			"content.ui": "<content><dropdown><button click=\"toggle()\">Mở</button><popover show=\"true\"><text>a</text></popover></dropdown></content>",
		},
		want: []string{
			"content.ui:1:65: popover hiện khi component chứa nó mở; không viết show (trang bỏ show này)",
		},
	},
	{
		name: "empty region",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"header.ui":  "<header></header>",
			"view.ui":    "<view><header /><content /></view>",
		},
		want: []string{
			"header.ui:1:1: <header> trong header.ui không có nội dung: trang dựng một <header> rỗng",
		},
	},
	{
		name: "empty region with a comment",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"footer.ui":  "<footer>\n  <!-- later -->\n</footer>",
			"view.ui":    "<view><footer /><content /></view>",
		},
		want: []string{
			"footer.ui:1:1: <footer> trong footer.ui không có nội dung: trang dựng một <footer> rỗng",
		},
	},
	{
		name: "a region with something",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"header.ui":  "<header><text>a</text></header>",
			"view.ui":    "<view><header /><content /></view>",
		},
	},
	{
		name: "warning in a for, once",
		files: map[string]string{
			"content.ui": "<content><list><item for=\"x of [1, 2, 3]\"><image value=\"/a.png\" /></item></list></content>",
		},
		want: []string{
			"content.ui:1:43: image cần alt (alt=\"\" nếu chỉ để trang trí)",
		},
	},
	{
		name: "the same warning in two places",
		files: map[string]string{
			"content.ui": "<content><image value=\"/a.png\" /><image value=\"/b.png\" /></content>",
		},
		want: []string{
			"content.ui:1:10: image cần alt (alt=\"\" nếu chỉ để trang trí)",
			"content.ui:1:34: image cần alt (alt=\"\" nếu chỉ để trang trí)",
		},
	},
	{
		name: "progressbar thick",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><progressbar thickness=\"12\" /><content /></view>",
		},
		drive: true,
		want: []string{
			"view.ui:1:20: thickness của progressbar là số px từ 1 đến 8 (mặc định 1); \"12\" dày hơn một vạch (trang vẽ đúng 12px)",
		},
	},
	{
		name: "progressbar slow",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><progressbar delay=\"20000\" /><content /></view>",
		},
		drive: true,
		want: []string{
			"view.ui:1:20: delay là số mili giây từ 0 đến 10000 (mặc định 120; 0 là hiện ngay); \"20000\" lâu tới mức thanh gần như không bao giờ hiện (trang giữ đúng 20000ms)",
		},
	},
	{
		name: "progressbar second",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><progressbar /><progressbar /><content /></view>",
		},
		drive: true,
		want: []string{
			"view.ui:1:22: page chỉ có một progressbar (thanh này chạy cùng thanh đầu)",
		},
	},
	{
		name: "progressbar not first",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"header.ui":  "<header><text>h</text></header>",
			"view.ui":    "<view><header /><progressbar /><content /></view>",
		},
		drive: true,
		want: []string{
			"view.ui:1:17: progressbar đứng đầu page, trên <header />: vạch nằm ở mép trên của trang",
		},
	},
	{
		name: "progressbar on a site that reloads",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><progressbar /><content /></view>",
		},
		want: []string{
			"view.ui:1:7: router.reload(true) tải lại cả trang mỗi lần chuyển nên progressbar không bao giờ chạy; bỏ thẻ này",
		},
	},
	{
		name: "reader twice",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><content /><reader /><reader /></view>",
		},
		drive: true,
		want: []string{
			"view.ui:1:28: page chỉ có một reader (trình đọc màn hình có thể đọc một câu hai lần)",
		},
	},
	{
		name: "reader in content",
		files: map[string]string{
			"content.ui": "<content><reader /><text>a</text></content>",
		},
		drive: true,
		want: []string{
			"content.ui:1:10: reader đặt trong view.ui, là con của <view>; không đặt trong vùng hay nội dung (Drive thay nội dung khi chuyển trang nên câu đọc có thể mất)",
		},
	},
	{
		name: "reader root of a piece",
		files: map[string]string{
			"content.ui":     "<content><text>a</text></content>",
			"page-reader.ui": "<reader />",
			"view.ui":        "<view><content /><page-reader /></view>",
		},
		drive: true,
		want: []string{
			"page-reader.ui:1:1: gốc của page-reader.ui là <reader>: reader đặt thẳng trong view.ui, một lần",
		},
	},
	{
		name: "toast clean",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><content /><toast as=\"$toast\"><card for=\"notice of notices; key: notice.id\"><text>{{ notice.message }}</text><button click=\"dismiss(notice.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button></card></toast></view>",
		},
	},
	{
		name: "toast without as",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><content /><toast><card for=\"notice of notices; key: notice.id\"><text>{{ notice.message }}</text><button click=\"dismiss(notice.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button></card></toast></view>",
		},
		want: []string{
			"view.ui:1:18: toast không có as: trang không gọi được show; viết <toast as=\"$toast\"> rồi gọi $toast.success('Đã lưu.') ở bất cứ đâu",
		},
	},
	{
		name: "toast without message",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><content /><toast as=\"$toast\"><card for=\"notice of notices; key: notice.id\"><text>Đã lưu</text><button click=\"dismiss(notice.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button></card></toast></view>",
		},
		want: []string{
			"view.ui:1:37: mẫu thông báo không hiện notice.message: thông báo sẽ không có chữ; viết <text>{{ notice.message }}</text>",
		},
	},
	{
		name: "toast in glass",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><box material=\"glass\"><toast as=\"$toast\"><card for=\"notice of notices; key: notice.id\"><text>{{ notice.message }}</text><button click=\"dismiss(notice.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button></card></toast></box><content /></view>",
		},
		want: []string{
			"view.ui:1:29: toast trong khung kính (material ở view.ui:1:12) bám theo khung đó, không theo màn hình: kính giữ phần cố định bên trong theo chính nó; đặt toast ngoài khung kính, như trong overlay.ui",
		},
	},
	{
		name: "toast in lazy",
		files: map[string]string{
			"content.ui": "<content><text>a</text></content>",
			"view.ui":    "<view><section lazy=\"true\"><toast as=\"$toast\"><card for=\"notice of notices; key: notice.id\"><text>{{ notice.message }}</text><button click=\"dismiss(notice.id)\"><icon value=\"x\" alt=\"Đóng thông báo\" /></button></card></toast></section><content /></view>",
		},
		want: []string{
			"view.ui:1:16: lazy=\"true\": khung này giữ <toast> ở view.ui:1:28; một khung lazy giữ phần cố định (position:fixed) bên trong theo chính nó, không theo màn hình, nên ngăn thông báo nằm ngoài màn hình: đặt lazy trên khung bên trong không giữ nó, hoặc đưa nó ra ngoài khung lazy (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "network empty place",
		files: map[string]string{
			"content.ui": "<content><network place=\"bottom\" /><text>a</text></content>",
		},
		want: []string{
			"content.ui:1:19: place=\"bottom\" trên <network /> rỗng: nó chỉ nghe sự kiện và không hiện, place không đặt gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ place",
		},
	},
	{
		name: "network empty duration",
		files: map[string]string{
			"content.ui": "<content><network duration=\"3s\" /><text>a</text></content>",
		},
		want: []string{
			"content.ui:1:19: duration=\"3s\" trên <network /> rỗng: nó không hiện gì, duration không đóng gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ duration",
		},
	},
	{
		name: "network empty duration with as",
		files: map[string]string{
			"content.ui": "<content><network as=\"$net\" duration=\"3s\" /><text>a</text></content>",
		},
	},
	{
		name: "network empty transition",
		files: map[string]string{
			"content.ui": "<content><network transition=\"fade\" /><text>a</text></content>",
		},
		want: []string{
			"content.ui:1:19: transition=\"fade\" trên <network /> rỗng: nó không hiện gì, transition không làm gì; vẽ thông báo bên trong (<card show=\"opened\">…</card>) hay bỏ transition",
		},
	},
	{
		name: "network quick",
		files: map[string]string{
			"content.ui": "<content><network duration=\"1s\" place=\"bottom\"><card show=\"opened\"><text>Đã có mạng lại</text></card></network><text>a</text></content>",
		},
		want: []string{
			"content.ui:1:19: duration=\"1s\" dưới 2 giây: người xem đọc không kịp; viết duration từ \"2s\", hay bỏ duration (4 giây)",
		},
	},
	{
		name: "network placed in lazy",
		files: map[string]string{
			"content.ui": "<content><section lazy=\"true\"><network place=\"bottom\"><card show=\"opened\"><text>Mất mạng</text></card></network></section></content>",
		},
		want: []string{
			"content.ui:1:19: lazy=\"true\": khung này giữ <network place=\"bottom\"> ở content.ui:1:31; một khung lazy giữ phần cố định (position:fixed) bên trong theo chính nó, không theo màn hình, nên ngăn thông báo nằm ngoài màn hình: đặt lazy trên khung bên trong không giữ nó, hoặc đưa nó ra ngoài khung lazy (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "network placed in glass",
		files: map[string]string{
			"content.ui": "<content><box material=\"glass\"><network place=\"bottom\"><card show=\"opened\"><text>Mất mạng</text></card></network></box></content>",
		},
		want: []string{
			"content.ui:1:32: network có place trong khung kính (material ở content.ui:1:15) bám theo khung đó, không theo màn hình: kính giữ phần cố định bên trong theo chính nó; đặt network ngoài khung kính, như trong overlay.ui",
		},
	},
	{
		name: "tabs clean",
		files: map[string]string{
			"content.ui": "<content><tabs as=\"$t\"><button click=\"select('a')\">A</button><button click=\"select('b')\">B</button></tabs><box show=\"$t.selected == 'a'\"><text>1</text></box><box show=\"$t.selected == 'b'\"><text>2</text></box></content>",
		},
	},
	{
		name: "tabs without as",
		files: map[string]string{
			"content.ui": "<content><tabs><button click=\"select('a')\">A</button><button click=\"select('b')\">B</button></tabs></content>",
		},
		want: []string{
			"content.ui:1:10: tabs không có as: ngoài <tabs> không gì đọc được tab đang chọn; viết <tabs as=\"$tên\"> rồi viết show=\"$tên.selected == 'tên tab'\" trên panel của mỗi tab",
		},
	},
	{
		name: "tabs a link inside",
		files: map[string]string{
			"content.ui": "<content><tabs as=\"$t\"><button click=\"select('a')\">A</button><link value=\"/x\">X</link></tabs><box show=\"$t.selected == 'a'\"><text>1</text></box></content>",
		},
		want: []string{
			"content.ui:1:62: <link> trong <tabs> không phải tab: dải tab chỉ chứa tab (role=\"tablist\"), phím mũi tên bỏ qua nó mà trình đọc màn hình đọc nó như một phần của dải; đặt nó ra ngoài <tabs>, bên cạnh dải tab",
		},
	},
	{
		name: "tabs a tab without panel",
		files: map[string]string{
			"content.ui": "<content><tabs as=\"$t\"><button click=\"select('a')\">A</button><button click=\"select('b')\">B</button></tabs><box show=\"$t.selected == 'a'\"><text>1</text></box></content>",
		},
		want: []string{
			"content.ui:1:70: tab b của $t không có panel: viết show=\"$t.selected == 'b'\" trên phần tử nó cho hiện",
		},
	},
	{
		name: "tabs a panel of no tab",
		files: map[string]string{
			"content.ui": "<content><tabs as=\"$t\"><button click=\"select('a')\">A</button></tabs><box show=\"$t.selected == 'a'\"><text>1</text></box><box show=\"$t.selected == 'z'\"><text>2</text></box></content>",
		},
		want: []string{
			"content.ui:1:125: show so $t.selected với 'z', mà $t không có tab z (a): phần tử này không bao giờ hiện",
		},
	},
	{
		name: "tabs select of a name from data",
		files: map[string]string{
			"content.ui": "<content><tabs as=\"$t\"><button click=\"select('a')\">A</button><button click=\"select(selected)\">B</button></tabs><box show=\"$t.selected == 'a'\"><text>1</text></box></content>",
		},
		want: []string{
			"content.ui:1:70: click=\"select(selected)\": engine nhận một nút là tab khi click là select('tên') với tên viết sẵn; nút này chọn được tab nhưng không phải tab (không có role=\"tab\", không đi bằng phím mũi tên)",
		},
	},
	{
		name: "carousel quick",
		files: map[string]string{
			"content.ui": "<content><carousel duration=\"1s\"><list><item><text>1</text></item><item><text>2</text></item></list><button click=\"toggle()\">Dừng</button></carousel></content>",
		},
		want: []string{
			"content.ui:1:20: duration=\"1s\" dưới 2 giây: người xem đọc không kịp một trang; viết duration từ \"2s\"",
		},
	},
	{
		name: "carousel never stopped",
		files: map[string]string{
			"content.ui": "<content><carousel duration=\"5s\"><list><item><text>1</text></item><item><text>2</text></item></list></carousel></content>",
		},
		want: []string{
			"content.ui:1:20: carousel tự chạy (duration=\"5s\") mà trang không có cách dừng: nội dung tự chuyển cần một nút dừng (WCAG 2.2.2); viết <button click=\"toggle()\"> trong <carousel>, hay gọi $…pause() ở chỗ khác",
		},
	},
	{
		name: "carousel stopped by its alias",
		files: map[string]string{
			"content.ui": "<content><carousel as=\"$c\" duration=\"5s\"><list><item><text>1</text></item><item><text>2</text></item></list></carousel><button click=\"$c.pause()\">Dừng</button></content>",
		},
	},
	{
		name: "carousel dot selected",
		files: map[string]string{
			"content.ui": "<content><carousel><list><item><text>1</text></item><item><text>2</text></item></list><button click=\"go(0)\" selected:background=\"brand\">1</button><button click=\"go(1)\">2</button></carousel></content>",
		},
		want: []string{
			"content.ui:1:109: selected:background: một chấm của carousel là trang đang hiện khi go(…) trỏ tới nó, và vẽ bằng active: (aria-current), không phải selected: (của tab); viết active:background",
		},
	},
	{
		name: "countdown without finish",
		files: map[string]string{
			"content.ui": "<content><countdown duration=\"60s\" ready=\"play()\"><time>{{ remaining / 1000 }}</time></countdown></content>",
		},
		want: []string{
			"content.ui:1:10: countdown không có finish: hết giờ thì người dùng trình đọc màn hình không biết (role=\"timer\" không đọc số đếm); viết finish=\"$toast.info('Hết giờ')\" hay finish=\"$reader.say('Hết giờ')\"",
		},
	},
	{
		name: "countdown never starts",
		files: map[string]string{
			"content.ui": "<content><countdown duration=\"60s\" finish=\"play()\"><time>{{ remaining / 1000 }}</time></countdown></content>",
		},
		want: []string{
			"content.ui:1:10: countdown không bao giờ chạy: không có ready=\"play()\", không nút nào gọi play() hay toggle(); viết ready=\"play()\" để chạy khi trang mở",
		},
	},
	{
		name: "countdown started by a button",
		files: map[string]string{
			"content.ui": "<content><countdown duration=\"60s\" finish=\"play()\"><time>{{ remaining / 1000 }}</time><button click=\"toggle()\">Chạy</button></countdown></content>",
		},
	},
	{
		name: "countdown started through its alias",
		files: map[string]string{
			"content.ui": "<content><countdown as=\"$tea\" duration=\"60s\" finish=\"play()\"><time>{{ remaining / 1000 }}</time></countdown><button click=\"$tea.play()\">Chạy</button></content>",
		},
	},
	{
		name: "terminal slow",
		files: map[string]string{
			"content.ui": "<content><terminal typing=\"8s\"><code language=\"bash\">ls</code></terminal></content>",
		},
		want: []string{
			"content.ui:1:20: typing=\"8s\" gõ lâu hơn 5 giây: chuyển động tự chạy quá 5 giây cần một cách để dừng (WCAG 2.2.2 Pause, Stop, Hide); viết typing=\"5s\" hoặc ngắn hơn",
		},
	},
	{
		name: "as on a layout",
		files: map[string]string{
			"content.ui":   "<content><shop-card as=\"$card\" /></content>",
			"shop-card.ui": "<card><text>a</text></card>",
		},
		want: []string{
			"content.ui:1:21: as đặt tên cho một instance; shop-card là layout (chỉ có giao diện, không có logic) nên không có instance để gọi: bỏ as",
		},
	},
	{
		name: "width show on a text",
		files: map[string]string{
			"content.ui": "<content><dropdown><button click=\"toggle()\">Mở</button><text show=\"opened\" medium:show=\"true\">a</text></dropdown></content>",
		},
		want: []string{
			"content.ui:1:76: medium:show=\"true\" bên cạnh show từ dữ liệu không hiện lại một <text>: thuộc tính hidden của show giấu nó ở mọi độ rộng, vì nó không có cách hiện của riêng nó để trả lại (khung, image, icon, badge, button thì có); đặt show và medium:show lên một <box> bọc nó",
		},
	},
	{
		name: "width show on a box",
		files: map[string]string{
			"content.ui": "<content><dropdown><button click=\"toggle()\">Mở</button><box show=\"opened\" medium:show=\"true\"><text>a</text></box></dropdown></content>",
		},
	},
	{
		name: "a name two components declare",
		files: map[string]string{
			"content.ui": "<content><dropdown><button click=\"toggle()\">Mở</button><popover><dropdown><button click=\"toggle()\">Con</button><popover><text>b</text></popover></dropdown></popover></dropdown></content>",
		},
		want: []string{
			"content.ui:1:83: toggle ở đây là của dropdown, mà dropdown bao ngoài cũng khai toggle: một tên một nghĩa — gọi qua alias ($tên.toggle) hoặc đổi tên",
		},
	},
	{
		name: "popover in lazy",
		files: map[string]string{
			"content.ui": "<content><section lazy=\"true\"><dropdown><button click=\"toggle()\">Mở</button><popover><text>a</text></popover></dropdown></section></content>",
		},
		want: []string{
			"content.ui:1:19: lazy=\"true\": khung này giữ <popover> ở content.ui:1:77; lazy cắt những gì vẽ ra ngoài khung (content-visibility:auto chứa cả phần vẽ trong khung), nên phần nổi ra ngoài mép khung bị cắt mất: đặt lazy trên khung bên trong không giữ nó, hoặc đưa nó ra ngoài khung lazy (trang vẫn vẽ lazy như đã viết)",
		},
	},
	{
		name: "placed below in lazy",
		files: map[string]string{
			"content.ui": "<content><section lazy=\"true\"><box><text place=\"below\">a</text></box></section></content>",
		},
	},
	{
		name: "editor no buttons",
		files: map[string]string{
			"content.ui": "<content><form method=\"post\"><field><label>Bài</label><editor as=\"$e\"><textarea name=\"body\" /></editor></field></form></content>",
		},
		want: []string{
			"content.ui:1:55: editor không có nút nào: điện thoại không có phím tắt; vẽ ít nhất <button click=\"toggle('bold')\"> và <button click=\"link()\"> trong một <box alt=\"Định dạng\">",
		},
	},
	{
		name: "editor alt beside label",
		files: map[string]string{
			"content.ui": "<content><form method=\"post\"><field><label>Bài</label><editor as=\"$e\"><box alt=\"Định dạng\"><button click=\"toggle('bold')\">B</button></box><textarea name=\"body\" alt=\"Bài viết\" /></editor></field></form></content>",
		},
		want: []string{
			"content.ui:1:161: alt trên <textarea> không có tác dụng: <label> của <field> bao ngoài đã là tên của vùng viết (trang bỏ alt này)",
		},
	},
	{
		name: "editor pressed",
		files: map[string]string{
			"content.ui": "<content><form method=\"post\"><field><label>Bài</label><editor as=\"$e\"><box alt=\"Định dạng\"><button click=\"toggle('bold')\" pressed:background=\"brand\">B</button></box><textarea name=\"body\" /></editor></field></form></content>",
		},
		want: []string{
			"content.ui:1:123: pressed: là lúc đang nhấn; nút đang bật định dạng tô bằng active: (active:background)",
		},
	},
	{
		name: "editor popover never opened",
		files: map[string]string{
			"content.ui": "<content><form method=\"post\"><field><label>Bài</label><editor as=\"$e\"><box alt=\"Định dạng\"><button click=\"toggle('bold')\">B</button></box><textarea name=\"body\" /><popover><input value=\"{{ address }}\" alt=\"Địa chỉ liên kết\" /><button click=\"link(address)\">Gắn</button></popover></editor></field></form></content>",
		},
		want: []string{
			"content.ui:1:163: popover của editor chỉ mở bằng link(): chưa có nút <button click=\"link()\">, nên khung liên kết không bao giờ hiện",
		},
	},
	{
		name: "editor submit held by empty",
		files: map[string]string{
			"content.ui": "<content><form method=\"post\"><field><label>Bài</label><editor as=\"$e\"><box alt=\"Định dạng\"><button click=\"toggle('bold')\">B</button></box><textarea name=\"body\" /></editor></field><button type=\"submit\" disabled=\"{{ $e.empty }}\">Gửi</button></form></content>",
		},
		want: []string{
			"content.ui:1:202: nút gửi khoá theo $e.empty: không có JavaScript thì không gửi được; required=\"true\" trên <textarea> đã chặn bài rỗng",
		},
	},
}

window.kitwork.component("html-playground", {
  content: "<p>Xin chào <strong>Kitwork</strong>.</p><ul><li>HTML đã lọc</li><li>State cập nhật ngay</li></ul>",
  example: function () {
    this.content = '<h2>Nội dung mẫu</h2><p>Đây là <strong>chữ đậm</strong>, <em>chữ nghiêng</em> và <code>code</code>.</p><p><a href="https://example.com">Liên kết HTTPS</a></p>';
  },
  attack: function () {
    this.content = '<script>alert("Không được chạy")</script><img src="/not-loaded" onerror="alert(1)"><a href="javascript:alert(1)">URL nguy hiểm bị bỏ</a><div data-kitwork-event="click: clear()" data-kitwork-component="html-playground">Directive bị bỏ; đây chỉ là nội dung.</div>';
  },
  clear: function () {
    this.content = "";
  }
});

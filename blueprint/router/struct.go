// Package router là bản thiết kế của một router.work (owner 24/11/2026, blueprint/README.md "Route"): chạy file chỉ
// dựng một Route, engine đọc cái file return. Như Website, Route chỉ giữ; khác Website, các chữ của nó kiểm đối số ngay
// khi được gọi (method.go), vì lỗi của một chữ lúc ấy mang file:dòng:cột của chính dòng viết sai trong router.work. Một
// chuỗi luật không có chữ kết thúc chỉ thấy được khi file đã chạy xong: engine kiểm nó khi nạp (Dangling).
package router

import (
	"time"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Route là bản thiết kế của một router.work: meta, và các chuỗi theo thứ tự viết. Mỗi dòng bắt đầu từ route là một
// chuỗi: các luật (guard, limit, cache) rồi một chữ kết thúc: một cửa (get, post…), all(…) hay methods(…).
type Route struct {
	meta   value.Value
	chains []*Chain
	shared bool // engine đã nạp và đóng băng (Share): từ đây chỉ đọc
}

// Chain là một chuỗi luật và chữ kết thúc của nó.
type Chain struct {
	route *Route
	rules []Rule
	end   End
}

// Rule là một luật của chuỗi, theo thứ tự viết.
type Rule struct {
	Kind  string      // "guard", "limit", "cache"
	Value value.Value // guard: hàm (context) => …; limit: object đã đọc; cache: hạn như người viết viết
	// limit, đã đọc: Rate request mỗi Per, đếm theo By ("ip")
	Rate int
	Per  time.Duration
	By   string
}

// End là chữ kết thúc của một chuỗi.
type End struct {
	Kind    string      // "" (chưa có), "door", "all", "methods"
	Method  string      // door: "get", "post", "put", "patch", "delete"
	Handler value.Value // door: hàm (context) => câu trả lời
	Methods []string    // all, methods: các method, hay ["*"]
}

// Meta là router({ meta }) như người viết viết; undefined khi không có. Không phải chữ JS.
func (r *Route) Meta() value.Value { return r.meta }

// Chains là các chuỗi của r theo thứ tự viết. Không phải chữ JS.
func (r *Route) Chains() []*Chain { return r.chains }

// Rules là các luật của c theo thứ tự viết; End là chữ kết thúc của nó. Không phải chữ JS.
func (c *Chain) Rules() []Rule { return c.rules }
func (c *Chain) End() End      { return c.end }

// Share là value.Sharer: engine gọi nó khi đóng băng route sau khi nạp: route chỉ đọc từ đây, và trả mọi giá trị nó giữ
// (hàm, object) để chúng được đóng băng tiếp; lần sau trả nil.
func (r *Route) Share() []value.Value {
	if r.shared {
		return nil
	}
	r.shared = true
	held := []value.Value{r.meta}
	for _, c := range r.chains {
		for _, rule := range c.rules {
			held = append(held, rule.Value)
		}
		held = append(held, c.end.Handler)
	}
	return held
}

// Shared là engine đã nạp r. Không phải chữ JS.
func (r *Route) Shared() bool { return r.shared }

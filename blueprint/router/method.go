package router

import (
	"slices"
	"strconv"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Chữ JS của Route và Chain. Một dòng bắt đầu từ route mở một chuỗi: guard, limit, cache thêm luật và trả chính chuỗi;
// một cửa (get, post, put, patch, delete), all(…) hay methods(…) kết thúc nó và trả route. Chữ nào cũng kiểm đối số ngay,
// để lỗi mang chỗ của dòng viết sai.

// doorMethods là các cửa và các tên method all(…), methods(…) nhận.
var doorMethods = []string{"get", "post", "put", "patch", "delete"}

// New là router(…): router(), hay router({ meta: { … } }).
func New(options ...value.Value) value.Value {
	r := &Route{}
	if len(options) > 1 {
		return value.Fail(value.TypeCode, "router takes router() or router({ meta }), got %d arguments", len(options))
	}
	if len(options) == 1 && !options[0].IsUndefined() {
		o := options[0]
		if !o.IsObject() {
			return value.Fail(value.TypeCode, "router takes router() or router({ meta }), got router(%s)", o.Typeof())
		}
		for _, key := range o.Keys() {
			item, _ := o.Get(key)
			switch key {
			case "meta":
				if !item.IsObject() {
					return value.Fail(value.TypeCode, "router({ meta }): meta is an object, like { title: \"…\" }, got %s", item.Typeof())
				}
				r.meta = item
			default:
				return value.Fail(value.TypeCode, "router has no key %q: meta", key)
			}
		}
	}
	return value.Struct(r)
}

// chain mở một chuỗi mới của r, ở cuối các chuỗi.
func (r *Route) chain() *Chain {
	c := &Chain{route: r}
	r.chains = append(r.chains, c)
	return c
}

// --- Chữ của Route: mỗi chữ mở một chuỗi rồi gọi chữ cùng tên của nó ---

func (r *Route) Guard(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Guard(v) })
}
func (r *Route) Limit(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Limit(v) })
}
func (r *Route) Cache(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Cache(v) })
}
func (r *Route) Get(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Get(v) })
}
func (r *Route) Post(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Post(v) })
}
func (r *Route) Put(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Put(v) })
}
func (r *Route) Patch(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Patch(v) })
}
func (r *Route) Delete(v value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Delete(v) })
}
func (r *Route) All(methods ...value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.All(methods...) })
}
func (r *Route) Methods(methods ...value.Value) value.Value {
	return r.open(func(c *Chain) value.Value { return c.Methods(methods...) })
}

func (r *Route) open(word func(*Chain) value.Value) value.Value {
	if r.shared {
		return readOnly()
	}
	return word(r.chain())
}

// --- Chữ của Chain: luật ---

// Guard thêm một guard: hàm (context) => context.next(), một câu trả lời, …
func (c *Chain) Guard(v value.Value) value.Value {
	if failure := c.writable(); failure.IsInvalid() {
		return failure
	}
	if !v.IsFunction() {
		return value.Fail(value.TypeCode, "route.guard(fn) takes a function (context) => context.next() | an answer, got %s", v.Typeof())
	}
	c.rules = append(c.rules, Rule{Kind: "guard", Value: v})
	return value.Struct(c)
}

// Limit thêm một giới hạn tần suất: { rate, per, by }: rate request mỗi per ("30s", "1m": value.TimeSpan), đếm theo by
// ("ip", cũng là mặc định).
func (c *Chain) Limit(v value.Value) value.Value {
	if failure := c.writable(); failure.IsInvalid() {
		return failure
	}
	rule, failure := readLimit(v)
	if failure.IsInvalid() {
		return failure
	}
	c.rules = append(c.rules, rule)
	return value.Struct(c)
}

// Cache thêm hạn cache của cửa GET đứng sau nó: "5m", "1h" (value.TimeSpan, owner 23/11), hay không có (tới khi site
// dựng lại).
func (c *Chain) Cache(v value.Value) value.Value {
	if failure := c.writable(); failure.IsInvalid() {
		return failure
	}
	if !v.IsUndefined() {
		if _, err := v.TimeSpan(); err != nil {
			return value.Fail(value.TypeCode, "route.cache(time): %v", err)
		}
	}
	c.rules = append(c.rules, Rule{Kind: "cache", Value: v})
	return value.Struct(c)
}

// --- Chữ của Chain: kết thúc ---

func (c *Chain) Get(v value.Value) value.Value    { return c.door("get", v) }
func (c *Chain) Post(v value.Value) value.Value   { return c.door("post", v) }
func (c *Chain) Put(v value.Value) value.Value    { return c.door("put", v) }
func (c *Chain) Patch(v value.Value) value.Value  { return c.door("patch", v) }
func (c *Chain) Delete(v value.Value) value.Value { return c.door("delete", v) }

// All kết thúc chuỗi cho các method ấy ở thư mục này và mọi thư mục con.
func (c *Chain) All(methods ...value.Value) value.Value { return c.ends("all", methods) }

// Methods kết thúc chuỗi cho các method ấy, chỉ ở thư mục này.
func (c *Chain) Methods(methods ...value.Value) value.Value { return c.ends("methods", methods) }

// door kết thúc chuỗi bằng một cửa: handler là hàm trả lời; mỗi method một cửa trong một router.work; cache chỉ trước
// một cửa GET.
func (c *Chain) door(method string, handler value.Value) value.Value {
	if failure := c.writable(); failure.IsInvalid() {
		return failure
	}
	if !handler.IsFunction() {
		return value.Fail(value.TypeCode, "route.%s(handler) takes a function (context) => an answer, got %s", method, handler.Typeof())
	}
	if method != "get" && c.has("cache") {
		return value.Fail(value.TypeCode, "route.cache(…) keeps the answer of a GET: write it before .get(…), not before .%s(…)", method)
	}
	for _, other := range c.route.chains {
		if other.end.Kind == "door" && other.end.Method == method {
			return value.Fail(value.TypeCode, "route.%s(…) is written twice in this router.work: one door per method", method)
		}
	}
	c.end = End{Kind: "door", Method: method, Handler: handler}
	return value.Struct(c.route)
}

// ends kết thúc chuỗi bằng all(…) hay methods(…): sau ít nhất một guard hay limit, các method là tên chữ thường hay "*".
func (c *Chain) ends(word string, arguments []value.Value) value.Value {
	if failure := c.writable(); failure.IsInvalid() {
		return failure
	}
	if c.has("cache") {
		return value.Fail(value.TypeCode, "route.cache(…) keeps the answer of one GET door: write it before .get(…), not before .%s(…)", word)
	}
	if len(c.rules) == 0 {
		return value.Fail(value.TypeCode, ".%s(…) ends a chain of rules: write route.guard(fn).%s(…) or route.limit({ … }).%s(…)", word, word, word)
	}
	methods, failure := readMethods(word, arguments)
	if failure.IsInvalid() {
		return failure
	}
	c.end = End{Kind: word, Methods: methods}
	return value.Struct(c.route)
}

// writable là undefined khi c còn ghi được: route chưa nạp, chuỗi chưa kết thúc.
func (c *Chain) writable() value.Value {
	switch {
	case c.route.shared:
		return readOnly()
	case c.end.Kind == "door":
		return value.Fail(value.TypeCode, "this chain already ended with .%s(…): rules stand before the door; start a new line from route", c.end.Method)
	case c.end.Kind != "":
		return value.Fail(value.TypeCode, "this chain already ended with .%s(…): start a new line from route", c.end.Kind)
	}
	return value.Value{}
}

// has là c có một luật loại kind.
func (c *Chain) has(kind string) bool {
	return slices.ContainsFunc(c.rules, func(r Rule) bool { return r.Kind == kind })
}

// readOnly là lỗi của một chữ ghi lên route đã nạp: một handler giữ biến route mà gọi route.get(…) lúc request.
func readOnly() value.Value {
	return value.Invalid("the route was loaded by the engine and is read only now: write it in router.work, not while serving")
}

// readMethods đọc các đối số của all(…), methods(…): "*" một mình, hay các tên khác nhau trong doorMethods.
func readMethods(word string, arguments []value.Value) ([]string, value.Value) {
	usage := `.all("*") is every method of this folder and every folder below it, .methods("*") of this folder only; ` +
		`or name them: .%s("get"), .%s("post", "delete")`
	if len(arguments) == 0 {
		return nil, value.Fail(value.TypeCode, ".%s() names the methods: "+usage, word, word, word)
	}
	var methods []string
	for _, a := range arguments {
		if !a.IsString() {
			return nil, value.Fail(value.TypeCode, ".%s(…) takes the names of methods as strings, got %s: "+usage, word, a.Typeof(), word, word)
		}
		name := a.String()
		switch {
		case name == "*" && len(arguments) > 1:
			return nil, value.Fail(value.TypeCode, `.%s("*") is every method already: write "*" alone`, word)
		case name == "*":
		case !slices.Contains(doorMethods, name):
			return nil, value.Fail(value.TypeCode, ".%s(%q): the methods are %s, in lower case, or \"*\"", word, name, strings.Join(doorMethods, ", "))
		case slices.Contains(methods, name):
			return nil, value.Fail(value.TypeCode, ".%s(…) names %q twice", word, name)
		}
		methods = append(methods, name)
	}
	return methods, value.Value{}
}

// readLimit đọc { rate, per, by } của limit.
func readLimit(v value.Value) (Rule, value.Value) {
	usage := `route.limit({ rate: 300, per: "1m" }): rate requests per a time like "30s" or "1m", counted by ip`
	if !v.IsObject() {
		return Rule{}, value.Fail(value.TypeCode, "%s, got %s", usage, v.Typeof())
	}
	rule := Rule{Kind: "limit", Value: v, By: "ip"}
	for _, key := range v.Keys() {
		item, _ := v.Get(key)
		switch key {
		case "rate":
			if !item.IsNumber() || item.Number() < 1 || item.Number() != float64(int(item.Number())) {
				return Rule{}, value.Fail(value.TypeCode, "route.limit({ rate }): rate is a whole number of requests, 1 or more, got %s", describe(item))
			}
			rule.Rate = int(item.Number())
		case "per":
			per, err := item.TimeSpan()
			if err != nil {
				return Rule{}, value.Fail(value.TypeCode, "route.limit({ per }): %v", err)
			}
			rule.Per = per
		case "by":
			if !item.IsString() || item.String() != "ip" {
				return Rule{}, value.Fail(value.TypeCode, `route.limit({ by }): requests are counted by "ip", got %s`, describe(item))
			}
		default:
			return Rule{}, value.Fail(value.TypeCode, "route.limit has no key %q: rate, per, by", key)
		}
	}
	if rule.Rate == 0 || rule.Per == 0 {
		return Rule{}, value.Fail(value.TypeCode, "%s: rate and per are both written", usage)
	}
	return rule, value.Value{}
}

// describe là một giá trị cho lời báo: chữ của một chuỗi, loại của thứ khác.
func describe(v value.Value) string {
	if v.IsString() {
		return strconv.Quote(v.String())
	}
	if v.IsNumber() {
		return strconv.FormatFloat(v.Number(), 'g', -1, 64)
	}
	return v.Typeof()
}

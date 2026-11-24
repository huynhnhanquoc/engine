// Package host là phần engine nạp bản thiết kế application.work. Hôm nay chỉ có listen: đọc, lọc, và các socket engine
// mở; bộ nạp các trường khác và bộ chia (mở socket, đọc byte đầu, TLS) chưa làm.
package host

import (
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/huynhnhanquoc/engine/core/value"
)

// Luật listen (owner 23/11, blueprint/README.md): các chữ chỉ thêm, bản thiết kế giữ đúng những gì người viết viết;
// engine đọc và lọc ở đây, khi nạp (nên .check cũng thấy), không lỗi, kể ra mỗi địa chỉ bỏ.
//
//   - Địa chỉ là một IP và một cổng. Host chưa chỉ định (":443", "0.0.0.0:443", "[::]:443") là một: mọi giao diện, IPv4
//     và IPv6, vì với "tcp" Go mở cả ba thành một socket (net.favoriteAddrFamily). localhost là 127.0.0.1 và ::1, cố
//     định (RFC 6761), không hỏi DNS; tên máy khác là lỗi, để boot không phụ thuộc DNS và .check ra như nhau mọi máy.
//   - Lớn chứa nhỏ: ":443" chứa mọi IP của cổng 443.
//   - Trong một cửa: trùng hay bị chứa thì bỏ (Filter); không mất gì, cửa đã có địa chỉ lớn.
//   - Giữa các cửa: chỉ lọc socket (Sockets); mỗi cửa giữ địa chỉ của nó, và bộ chia đưa cho một cửa chỉ kết nối đi vào
//     địa chỉ của nó (Use.Accepts), nên database("127.0.0.1:443") chung socket với website(443) mà không lộ ra ngoài.

// Address là một địa chỉ đã đọc: Text đã chuẩn hoá (host là một IP; mọi giao diện viết ":443"), At là chỗ người viết
// nêu nó ("application.website.listen[0]"), cho lời kể khi bỏ.
type Address struct {
	Text string
	At   string
}

// Addresses đọc items, các địa chỉ một trường listen giữ, theo thứ tự viết, chưa lọc; path là tên trường
// ("application.website.listen"). localhost thành hai địa chỉ. Một item đọc không được là một lỗi ở path[i], các item
// khác vẫn đọc.
func Addresses(path string, items []value.Value) ([]Address, []error) {
	var list []Address
	var errors []error
	for i, item := range items {
		at := fmt.Sprintf("%s[%d]", path, i)
		hostPort, err := item.HostPort()
		if err != nil {
			errors = append(errors, fmt.Errorf("%s: %v", at, err))
			continue
		}
		host, port, _ := net.SplitHostPort(hostPort)
		var hosts []string
		if strings.EqualFold(host, "localhost") {
			hosts = []string{"127.0.0.1", "::1"}
		} else if host == "" {
			hosts = []string{""}
		} else {
			ip, err := netip.ParseAddr(host)
			if err != nil {
				errors = append(errors, fmt.Errorf(`%s: %q is not an IP: listen takes an IP, like "127.0.0.1:443", or localhost`, at, host))
				continue
			}
			// ::ffff:127.0.0.1 là 127.0.0.1; 0.0.0.0 và :: là mọi giao diện
			if ip = ip.Unmap(); ip.IsUnspecified() {
				hosts = []string{""}
			} else {
				hosts = []string{ip.String()}
			}
		}
		for _, h := range hosts {
			list = append(list, Address{net.JoinHostPort(h, port), at})
		}
	}
	return list, errors
}

// Covers là a chứa b, cùng cổng: a là mọi giao diện (":443"), b là một IP ("127.0.0.1:443"). Một địa chỉ không chứa
// chính nó: trùng là việc của Filter.
func Covers(a, b string) bool {
	aHost, aPort, _ := net.SplitHostPort(a)
	bHost, bPort, _ := net.SplitHostPort(b)
	return aPort == bPort && aHost == "" && bHost != ""
}

// Dropped là một địa chỉ Filter bỏ: trùng By (cùng Text), hay By chứa nó.
type Dropped struct {
	Address Address
	By      Address
}

func (d Dropped) String() string {
	if d.Address.Text == d.By.Text {
		return fmt.Sprintf("%s %s is dropped: the same as %s", d.Address.At, d.Address.Text, d.By.At)
	}
	return fmt.Sprintf("%s %s is dropped: %s (%s) covers it", d.Address.At, d.Address.Text, d.By.Text, d.By.At)
}

// Filter lọc các địa chỉ của một cửa, theo thứ tự đọc: trùng thì giữ cái viết trước, cái bị một địa chỉ lớn hơn chứa thì
// bỏ, dù địa chỉ lớn viết sau. kept là các địa chỉ còn lại; dropped kể mỗi cái bỏ, để engine in ra (không ẩn).
func Filter(list []Address) (kept []string, dropped []Dropped) {
next:
	for i, a := range list {
		for j, b := range list {
			if j < i && b.Text == a.Text || Covers(b.Text, a.Text) {
				dropped = append(dropped, Dropped{a, b})
				continue next
			}
		}
		kept = append(kept, a.Text)
	}
	return kept, dropped
}

// Door là một cửa nghe cổng ("website", "database") và các địa chỉ của nó, đã lọc (Filter).
type Door struct {
	Name      string
	Addresses []string
}

// Socket là một socket engine mở, và các cửa dùng nó.
type Socket struct {
	Address string // ":443", "127.0.0.1:5445"
	Doors   []Use
}

// Use là một cửa trên một socket. Only rỗng: cửa nhận mọi kết nối của socket; không rỗng: chỉ kết nối đi vào các địa
// chỉ ấy (cửa nêu IP, socket là mọi giao diện của cổng ấy).
type Use struct {
	Door string
	Only []netip.AddrPort
}

// Sockets là các socket engine mở cho doors: hợp địa chỉ mọi cửa theo thứ tự gặp, địa chỉ bị địa chỉ lớn hơn chứa thì
// đi qua socket lớn. Mỗi cửa giữ địa chỉ của nó: một Use cho mỗi cửa trên socket.
func Sockets(doors []Door) []Socket {
	var all []string
	for _, d := range doors {
		for _, a := range d.Addresses {
			if !slices.Contains(all, a) {
				all = append(all, a)
			}
		}
	}
	var sockets []Socket
	for _, s := range all {
		if slices.ContainsFunc(all, func(b string) bool { return Covers(b, s) }) {
			continue // đi qua socket lớn
		}
		socket := Socket{Address: s}
		for _, d := range doors {
			whole, only := false, []netip.AddrPort(nil)
			for _, a := range d.Addresses {
				if a == s {
					whole = true
				} else if Covers(s, a) {
					only = append(only, netip.MustParseAddrPort(a)) // đã chuẩn hoá ở Addresses
				}
			}
			switch {
			case whole:
				socket.Doors = append(socket.Doors, Use{Door: d.Name})
			case len(only) > 0:
				socket.Doors = append(socket.Doors, Use{Door: d.Name, Only: only})
			}
		}
		sockets = append(sockets, socket)
	}
	return sockets
}

// Accepts là bộ chia được đưa cho cửa này một kết nối đi vào local (LocalAddr của kết nối). So theo IP, không theo
// byte: trên socket mọi giao diện, một kết nối IPv4 vào 127.0.0.1 đến với địa chỉ ::ffff:127.0.0.1 (Unmap).
func (u Use) Accepts(local netip.AddrPort) bool {
	if len(u.Only) == 0 {
		return true
	}
	address := local.Addr().Unmap()
	for _, only := range u.Only {
		if only.Addr().Unmap() == address && only.Port() == local.Port() {
			return true
		}
	}
	return false
}

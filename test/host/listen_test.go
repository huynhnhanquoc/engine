package host_test

import (
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/huynhnhanquoc/engine/core/value"
	"github.com/huynhnhanquoc/engine/host"
)

// Luật listen (host/listen.go): chữ chỉ thêm, engine đọc và lọc khi nạp; lớn chứa nhỏ; trong một cửa bỏ cái trùng hay bị
// chứa; giữa các cửa chỉ lọc socket, mỗi cửa giữ địa chỉ của nó.

func texts(list []host.Address) []string {
	out := make([]string, len(list))
	for i, a := range list {
		out[i] = a.Text
	}
	return out
}

func TestAddresses(t *testing.T) {
	for _, c := range []struct {
		name string
		item value.Value
		want []string
	}{
		{"a port", value.Number(443), []string{":443"}},
		{"an empty host", value.String(":443"), []string{":443"}},
		// với "tcp", Go mở 0.0.0.0 và :: thành cùng một socket như ":443"
		{"0.0.0.0", value.String("0.0.0.0:443"), []string{":443"}},
		{"::", value.String("[::]:443"), []string{":443"}},
		{"an IPv4", value.String("127.0.0.1:443"), []string{"127.0.0.1:443"}},
		{"an IPv6", value.String("[::1]:443"), []string{"[::1]:443"}},
		{"an IPv4 written as IPv6", value.String("[::ffff:127.0.0.1]:443"), []string{"127.0.0.1:443"}},
		{"a leading zero in the port", value.String(":0443"), []string{":443"}},
		// localhost cố định, không hỏi DNS
		{"localhost", value.String("localhost:5445"), []string{"127.0.0.1:5445", "[::1]:5445"}},
		{"LOCALHOST", value.String("LOCALHOST:5445"), []string{"127.0.0.1:5445", "[::1]:5445"}},
	} {
		list, errors := host.Addresses("application.listen", []value.Value{c.item})
		if len(errors) > 0 || !slices.Equal(texts(list), c.want) {
			t.Errorf("%s: %v %v, want %v", c.name, texts(list), errors, c.want)
		}
	}

	// mỗi địa chỉ nhớ chỗ người viết nêu nó; một item hỏng là một lỗi ở chỗ ấy, các item khác vẫn đọc
	list, errors := host.Addresses("application.website.listen", []value.Value{
		value.Number(443), value.String("example.com:443"), value.Number(70000), value.Boolean(true), value.String("localhost:80"),
	})
	if want := []string{":443", "127.0.0.1:80", "[::1]:80"}; !slices.Equal(texts(list), want) {
		t.Errorf("the items that read: %v, want %v", texts(list), want)
	}
	if list[0].At != "application.website.listen[0]" || list[1].At != "application.website.listen[4]" || list[2].At != "application.website.listen[4]" {
		t.Errorf("where each was written: %q %q %q", list[0].At, list[1].At, list[2].At)
	}
	want := []string{
		`application.website.listen[1]: "example.com" is not an IP: listen takes an IP, like "127.0.0.1:443", or localhost`,
		`application.website.listen[2]: port 70000 is not an integer from 1 to 65535`,
		`application.website.listen[3]: an address is a port 8080 or a string ":443", got boolean`,
	}
	if len(errors) != len(want) {
		t.Fatalf("errors: %v", errors)
	}
	for i, err := range errors {
		if err.Error() != want[i] {
			t.Errorf("error %d: %q, want %q", i, err.Error(), want[i])
		}
	}
}

func TestCovers(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{":443", "127.0.0.1:443", true},
		{":443", "[::1]:443", true},
		{":443", "127.0.0.1:8443", false}, // cổng khác
		{":443", ":443", false},           // chính nó: là trùng, không phải chứa
		{"127.0.0.1:443", ":443", false},  // nhỏ không chứa lớn
		{"127.0.0.1:443", "[::1]:443", false},
	} {
		if got := host.Covers(c.a, c.b); got != c.want {
			t.Errorf("Covers(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestFilter(t *testing.T) {
	// app.website().listen(443, "127.0.0.1:443", "0.0.0.0:8080", 8080, "localhost:8443", ":8443"): các địa chỉ của một cửa
	read := func(path string, items ...value.Value) []host.Address {
		list, errors := host.Addresses(path, items)
		if len(errors) > 0 {
			t.Fatal(errors)
		}
		return list
	}
	list := append(
		read("application.website.listen", value.Number(443), value.String("127.0.0.1:443"), value.String("0.0.0.0:8080")),
		read("application.listen", value.Number(8080), value.String("localhost:8443"), value.String(":8443"))...)
	kept, dropped := host.Filter(list)
	if want := []string{":443", ":8080", ":8443"}; !slices.Equal(kept, want) {
		t.Errorf("kept %v, want %v", kept, want)
	}
	var said []string
	for _, d := range dropped {
		said = append(said, d.String())
	}
	want := []string{
		"application.website.listen[1] 127.0.0.1:443 is dropped: :443 (application.website.listen[0]) covers it",
		"application.listen[0] :8080 is dropped: the same as application.website.listen[2]",
		// địa chỉ lớn viết sau vẫn chứa cái nhỏ viết trước
		"application.listen[1] 127.0.0.1:8443 is dropped: :8443 (application.listen[2]) covers it",
		"application.listen[1] [::1]:8443 is dropped: :8443 (application.listen[2]) covers it",
	}
	if !slices.Equal(said, want) {
		t.Errorf("dropped:\n%s\nwant:\n%s", strings.Join(said, "\n"), strings.Join(want, "\n"))
	}

	// không có địa chỉ lớn thì các IP đều giữ, trùng thì giữ cái đầu
	kept, dropped = host.Filter(read("x", value.String("127.0.0.1:5445"), value.String("[::1]:5445"), value.String("127.0.0.1:5445")))
	if !slices.Equal(kept, []string{"127.0.0.1:5445", "[::1]:5445"}) || len(dropped) != 1 || dropped[0].By.At != "x[0]" {
		t.Errorf("IPs alone: %v, %v", kept, dropped)
	}
}

func TestSockets(t *testing.T) {
	// app.website().listen(443, 8080); database nêu 8080 và localhost:443
	sockets := host.Sockets([]host.Door{
		{Name: "website", Addresses: []string{":443", ":8080"}},
		{Name: "database", Addresses: []string{":8080", "127.0.0.1:443", "[::1]:443"}},
	})
	if len(sockets) != 2 || sockets[0].Address != ":443" || sockets[1].Address != ":8080" {
		t.Fatalf("one socket a port, the covered addresses go through it: %+v", sockets)
	}
	on443 := sockets[0].Doors
	if len(on443) != 2 || on443[0].Door != "website" || len(on443[0].Only) != 0 ||
		on443[1].Door != "database" || len(on443[1].Only) != 2 ||
		on443[1].Only[0] != netip.MustParseAddrPort("127.0.0.1:443") || on443[1].Only[1] != netip.MustParseAddrPort("[::1]:443") {
		t.Errorf(":443: website whole, database only on the machine: %+v", on443)
	}
	if on8080 := sockets[1].Doors; len(on8080) != 2 || len(on8080[0].Only) != 0 || len(on8080[1].Only) != 0 {
		t.Errorf(":8080: both doors whole: %+v", on8080)
	}

	// một cửa chỉ nêu IP: socket là chính IP ấy
	sockets = host.Sockets([]host.Door{{Name: "database", Addresses: []string{"127.0.0.1:5445", "[::1]:5445"}}})
	if len(sockets) != 2 || sockets[0].Address != "127.0.0.1:5445" || sockets[1].Address != "[::1]:5445" || len(sockets[0].Doors[0].Only) != 0 {
		t.Errorf("IPs alone: %+v", sockets)
	}
	if host.Sockets(nil) != nil {
		t.Error("no door, no socket")
	}
}

func TestAccepts(t *testing.T) {
	database := host.Use{Door: "database", Only: []netip.AddrPort{netip.MustParseAddrPort("127.0.0.1:443")}}
	for _, c := range []struct {
		name  string
		local netip.AddrPort
		want  bool
	}{
		{"into the machine", netip.MustParseAddrPort("127.0.0.1:443"), true},
		// socket mọi giao diện: kết nối IPv4 đến với địa chỉ IPv6 ánh xạ
		{"into the machine, through a dual socket", netip.AddrPortFrom(netip.MustParseAddr("::ffff:127.0.0.1"), 443), true},
		{"from outside", netip.MustParseAddrPort("203.0.113.5:443"), false},
		{"another port", netip.MustParseAddrPort("127.0.0.1:8443"), false},
	} {
		if got := database.Accepts(c.local); got != c.want {
			t.Errorf("%s: %v", c.name, got)
		}
	}
	if !(host.Use{Door: "website"}).Accepts(netip.MustParseAddrPort("203.0.113.5:443")) {
		t.Error("a door on the whole socket takes every connection")
	}
}

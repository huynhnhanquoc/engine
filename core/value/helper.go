package value

import (
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"
)

// Scan đọc giá trị của v vào biến Go mà target trỏ tới (*string, *bool, *int, *int64, *float64, *time.Time,
// *time.Duration, *[]byte). Khác loại, hay số lẻ/ngoài khoảng của *int và *int64, là lỗi không nêu tên trường để chỗ
// gọi bọc bằng value.Invalid("tên: %w", err).
func (v Value) Scan(target any) error {
	switch dst := target.(type) {
	case *string:
		if v.kind != text {
			return fmt.Errorf("takes a string, got %s", v.Typeof())
		}
		*dst = v.text()
		return nil
	case *bool:
		if v.kind != boolean {
			return fmt.Errorf("takes a boolean, got %s", v.Typeof())
		}
		*dst = v.number == 1
		return nil
	case *int:
		if v.kind != number {
			return fmt.Errorf("takes a number, got %s", v.Typeof())
		}
		n := v.Number()
		if n != math.Trunc(n) || n < math.MinInt32 || n > math.MaxInt32 {
			return fmt.Errorf("%v is not an integer", n)
		}
		*dst = int(n)
		return nil
	case *int64:
		if v.kind != number {
			return fmt.Errorf("takes a number, got %s", v.Typeof())
		}
		n := v.Number()
		if n != math.Trunc(n) || n < -(1<<53) || n > 1<<53 {
			return fmt.Errorf("%v is not an integer", n)
		}
		*dst = int64(n)
		return nil
	case *float64:
		if v.kind != number {
			return fmt.Errorf("takes a number, got %s", v.Typeof())
		}
		*dst = v.Number()
		return nil
	case *time.Time:
		if v.kind != datetime {
			return fmt.Errorf("takes a time, got %s", v.Typeof())
		}
		*dst = v.Time()
		return nil
	case *time.Duration:
		if v.kind != duration {
			return fmt.Errorf("takes a duration, got %s", v.Typeof())
		}
		*dst = v.Duration()
		return nil
	case *[]byte:
		if v.kind != bytes {
			return fmt.Errorf("takes bytes, got %s", v.Typeof())
		}
		*dst = v.Bytes()
		return nil
	}
	return fmt.Errorf("cannot scan into %T", target)
}

// HostPort là địa chỉ lắng nghe v viết, ở dạng "host:port" net.Listen nhận:
//
//	8080               số: cổng trên mọi giao diện, ":8080"
//	":443"             chuỗi: cổng trên mọi giao diện
//	"127.0.0.1:8080"   chuỗi: một giao diện; IPv6 trong ngoặc vuông, "[::1]:8080"
//
// Cổng là số nguyên từ 1 tới 65535, viết lại không số 0 đứng đầu (":080" là ":80"). Lỗi không nêu chữ nào gọi nó
// (listen của website hay của database): chỗ gọi thêm vào, value.Invalid("listen: %w", err).
func (v Value) HostPort() (string, error) {
	switch v.kind {
	case number:
		n := v.Number()
		if n < 1 || n > 65535 || n != math.Trunc(n) {
			return "", fmt.Errorf("port %v is not an integer from 1 to 65535", n)
		}
		return net.JoinHostPort("", strconv.Itoa(int(n))), nil
	case text:
		address := v.String()
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return "", fmt.Errorf(`%q is not an address: write 8080, ":443" or "127.0.0.1:8080"`, address)
		}
		n, err := strconv.ParseUint(port, 10, 16)
		if err != nil || n == 0 {
			return "", fmt.Errorf("port of %q is not an integer from 1 to 65535", address)
		}
		return net.JoinHostPort(host, strconv.FormatUint(n, 10)), nil
	}
	return "", fmt.Errorf(`an address is a port 8080 or a string ":443", got %s`, v.Typeof())
}

// ByteSize là số byte v viết, ở dạng số nguyên dương ghép ngay với đơn vị chữ thường kb, mb hay gb ("512kb", "64mb",
// "2gb"; 1 kb = 1024 byte). Dùng chung cho memory của application, website, schedule, desktop và mobile.
func (v Value) ByteSize() (int64, error) {
	if v.kind != text {
		return 0, fmt.Errorf(`a size is a string like "512kb", "64mb" or "2gb", got %s`, v.Typeof())
	}
	s := v.String()
	for _, unit := range []struct {
		suffix string
		shift  uint
	}{
		{"kb", 10},
		{"mb", 20},
		{"gb", 30},
	} {
		if digits, ok := strings.CutSuffix(s, unit.suffix); ok {
			n, err := strconv.ParseUint(digits, 10, 64)
			if err != nil || n == 0 || n > math.MaxInt64>>unit.shift {
				break
			}
			return int64(n << unit.shift), nil
		}
	}
	return 0, fmt.Errorf(`%q is not a size: write "512kb", "64mb" or "2gb"`, s)
}

// TimeSpan là khoảng thời gian v viết, ở dạng số nguyên dương ghép ngay với đơn vị chữ thường ms, s, m, h hay d
// ("500ms", "30s", "2m", "1h", "1d"; 1 d = 24 h), như ByteSize cho byte. Dùng chung cho timeout của database, và
// cache, persist, timeout của http, queue, lịch khi engine làm tới (owner 23/11). Chặt hơn time.ParseDuration của Go:
// một đơn vị ("1h30m" là lỗi), không số lẻ, không số âm, và có d, mà Go không có. Một giá trị loại duration dương cũng
// nhận.
func (v Value) TimeSpan() (time.Duration, error) {
	switch v.kind {
	case duration:
		if d := v.Duration(); d > 0 {
			return d, nil
		}
		return 0, fmt.Errorf("%v is not a positive time", v.Duration())
	case text:
	default:
		return 0, fmt.Errorf(`a time is a string like "500ms", "30s" or "2m", got %s`, v.Typeof())
	}
	s := v.String()
	for _, unit := range []struct {
		suffix string
		size   time.Duration
	}{
		{"ms", time.Millisecond}, // trước "s" và "m": "500ms" cũng tận cùng bằng "s"
		{"s", time.Second},
		{"m", time.Minute},
		{"h", time.Hour},
		{"d", 24 * time.Hour},
	} {
		if digits, ok := strings.CutSuffix(s, unit.suffix); ok {
			n, err := strconv.ParseUint(digits, 10, 64)
			if err != nil || n == 0 || n > uint64(math.MaxInt64/unit.size) {
				break
			}
			return time.Duration(n) * unit.size, nil
		}
	}
	return 0, fmt.Errorf(`%q is not a time: write "500ms", "30s", "2m", "1h" or "1d"`, s)
}

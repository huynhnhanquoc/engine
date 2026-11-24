package application

import (
	"strconv"
	"strings"

	"github.com/kitwork/engine2/core/value"
)

// Limits is app.limits(…): the tenants' limits the host sets (12/11/2026, docs/KITWORK_BOOT.md, "Giới hạn tenant"),
// none of which an app can raise. Memory is what one run may make (a request, its guards and its door together; a job,
// a routine, a file at boot), Energy what one run may spend (its instructions and what the engine does for it, counted
// by its work, 15/11/2026), Requests how many requests one site runs at once, Host what every run of the host makes
// together; 0 is the default of each: 64 MiB, 10,000,000 units (some 44 ms of the engine's work where measured), twice
// the processors (at least 8), none. They are every tenant's defaults: a tenant's own are its row in the host's table
// tenant (17/11/2026, slice 4 of root docs/KITWORK_MODULE_WORK.md; host tenants.go), which TenantLimits reads; until
// then app.limits("site", { … }) set one site's, now an error saying so.
type Limits struct {
	Memory   uint64
	Energy   uint64
	Requests int
	Host     uint64
	set      bool
}

// limitsHow is how app.limits(…) is written.
const limitsHow = `app.limits({ memory: "64mb", energy: 50_000_000, requests: 32, host: "2gb" }), the defaults of every tenant; a tenant's own are its row in the host's table tenant`

// the bounds of what app.limits(…) takes: a run below a megabyte cannot draw a page, a host below its runs' ceiling
// holds none of them
const (
	leastMemory   = 1 << 20
	mostMemory    = 16 << 30
	mostRequests  = 100_000
	mostHostBytes = 1 << 50
	// a run of 100,000 units draws a page (kitwork.io's spend 959 to 2,911); 10^12 is some 70 minutes of the engine's
	// work, past which a run is better a job of its own
	leastEnergy = 100_000
	mostEnergy  = 1_000_000_000_000
)

// limitValues are the limits one object of app.limits(…) names, 0 for each it leaves out.
type limitValues struct {
	memory, energy, host uint64
	requests             int
}

// Limits is app.limits({ memory, energy, requests, host }): the host's ceilings, every tenant's defaults, once.
// app.limits("site", { … }), one site's until 17/11/2026, is an error naming the table tenant.
func (a *Application) Limits(arguments ...value.Value) value.Value {
	if a.manifest.Run {
		return a.afterRun("limits(…)")
	}
	limits := a.manifest.Limits
	if limits == nil {
		limits = &Limits{}
	}
	switch {
	case len(arguments) == 1 && arguments[0].IsObject():
		if limits.set {
			return value.Fail(value.InvalidArgumentCode, "app.limits({ … }) is declared twice: the host's limits are one; a tenant's own are its row in the host's table tenant")
		}
		read, failure := readLimits(arguments[0], true)
		if failure.IsInvalid() {
			return failure
		}
		if read.host > 0 && read.memory > read.host {
			return value.Fail(value.InvalidArgumentCode, "app.limits({ … }): host "+sizeText(read.host)+" is less than one run's memory "+sizeText(read.memory)+": no run would fit")
		}
		limits.Memory, limits.Energy, limits.Requests, limits.Host, limits.set = read.memory, read.energy, read.requests, read.host, true
	case len(arguments) == 2 && arguments[0].IsString():
		return value.Fail(value.InvalidArgumentCode, "app.limits("+strconv.Quote(arguments[0].Text())+", { … }) is gone (17/11/2026): a tenant's own limits are its row in the host's table tenant of the system database, INSERT INTO tenant (identity, status, memory, energy, requests) VALUES ('<identity>', 'active', '128mb', 20000000, 8); app.limits({ … }) are every tenant's defaults")
	default:
		return value.Fail(value.InvalidArgumentCode, limitsHow)
	}
	a.manifest.Limits = limits
	return value.Struct(a)
}

// readLimits reads the object of app.limits(…): memory and host as sizes ("64mb", "2gb", a number of bytes), energy
// and requests whole numbers; host only for every site.
func readLimits(options value.Value, every bool) (limitValues, value.Value) {
	var read limitValues
	keys := options.Keys()
	if len(keys) == 0 {
		return read, value.Fail(value.InvalidArgumentCode, "app.limits(…) names no limit: "+limitsHow)
	}
	for _, key := range keys {
		item := options.Get(key)
		switch key {
		case "memory":
			size, ok := readLimitSize(item)
			if !ok || size < leastMemory || size > mostMemory {
				return read, value.Fail(value.InvalidArgumentCode, `app.limits(…): memory is what one run may make, from "1mb" to "16gb": memory: "64mb"`)
			}
			read.memory = size
		case "energy":
			if !item.IsNumber() || item.Number() != float64(uint64(item.Number())) || item.Number() < leastEnergy || item.Number() > mostEnergy {
				return read, value.Fail(value.InvalidArgumentCode, "app.limits(…): energy is what one run may spend, a whole number of units from 100_000 to 1_000_000_000_000; the default is 10_000_000, some 44 ms of the engine's work: energy: 50_000_000")
			}
			read.energy = uint64(item.Number())
		case "requests":
			if !item.IsNumber() || item.Number() != float64(int(item.Number())) || item.Number() < 1 || item.Number() > mostRequests {
				return read, value.Fail(value.InvalidArgumentCode, "app.limits(…): requests is how many requests a site runs at once, a whole number from 1: requests: 32")
			}
			read.requests = int(item.Number())
		case "host":
			if !every {
				return read, value.Fail(value.InvalidArgumentCode, `app.limits("site", { … }): host is one for every site, written in app.limits({ host: "2gb" })`)
			}
			size, ok := readLimitSize(item)
			if !ok || size < leastMemory || size > mostHostBytes {
				return read, value.Fail(value.InvalidArgumentCode, `app.limits(…): host is what every run of the host makes together, from "1mb": host: "2gb"`)
			}
			read.host = size
		default:
			names := []string{"memory", "energy", "requests"}
			if every {
				names = append(names, "host")
			}
			return read, value.Fail(value.InvalidArgumentCode, "app.limits(…) has no "+strconv.Quote(key)+": it takes "+strings.Join(names, ", ")+"; "+limitsHow)
		}
	}
	return read, value.Value{}
}

// TenantLimit is a tenant's own limits, a row of the host's table tenant (host tenants.go): 0 leaves the default.
type TenantLimit struct {
	Memory   uint64
	Energy   uint64
	Requests int
}

// TenantLimits reads the limits of a row of the host's table tenant as app.limits(…) reads its own: memory a size
// ("128mb", or "" for the default), energy and requests whole numbers (0 for the default). problem says what of it is
// wrong, "" when nothing is.
func TenantLimits(memory string, energy, requests int64) (TenantLimit, string) {
	var limit TenantLimit
	if strings.TrimSpace(memory) != "" {
		size, ok := readLimitSize(value.String(memory))
		if !ok || size < leastMemory || size > mostMemory {
			return limit, "memory " + strconv.Quote(memory) + " is no size from 1mb to 16gb"
		}
		limit.Memory = size
	}
	if energy != 0 {
		if energy < leastEnergy || energy > mostEnergy {
			return limit, "energy " + strconv.FormatInt(energy, 10) + " is not from 100000 to 1000000000000"
		}
		limit.Energy = uint64(energy)
	}
	if requests != 0 {
		if requests < 1 || requests > mostRequests {
			return limit, "requests " + strconv.FormatInt(requests, 10) + " is not from 1 to 100000"
		}
		limit.Requests = int(requests)
	}
	return limit, ""
}

// SizeText is a size as a person reads it, "64mb" (sizeText).
func SizeText(n uint64) string { return sizeText(n) }

// readLimitSize reads a size written as text ("512kb", "64mb", "2gb", "100b") or a whole number of bytes, as
// kitwork/http's maxBytes reads one.
func readLimitSize(item value.Value) (uint64, bool) {
	if item.IsNumber() {
		n := item.Number()
		if n < 1 || n != float64(uint64(n)) {
			return 0, false
		}
		return uint64(n), true
	}
	if !item.IsString() {
		return 0, false
	}
	text := strings.ToLower(strings.TrimSpace(item.Text()))
	for _, unit := range []struct {
		suffix string
		size   float64
	}{{"gb", 1 << 30}, {"mb", 1 << 20}, {"kb", 1 << 10}, {"b", 1}} {
		if number, ok := strings.CutSuffix(text, unit.suffix); ok {
			n, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
			if err != nil || n <= 0 || n*unit.size > float64(mostHostBytes) {
				return 0, false
			}
			return uint64(n * unit.size), true
		}
	}
	n, err := strconv.ParseUint(text, 10, 64)
	return n, err == nil && n > 0
}

// sizeText is a size as a person reads it: "64mb", "512kb", "100 bytes".
func sizeText(n uint64) string {
	switch {
	case n >= 1<<30 && n%(1<<30) == 0:
		return strconv.FormatUint(n>>30, 10) + "gb"
	case n >= 1<<20 && n%(1<<20) == 0:
		return strconv.FormatUint(n>>20, 10) + "mb"
	case n >= 1<<10 && n%(1<<10) == 0:
		return strconv.FormatUint(n>>10, 10) + "kb"
	}
	return strconv.FormatUint(n, 10) + " bytes"
}

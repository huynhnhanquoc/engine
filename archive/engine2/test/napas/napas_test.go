// Package napas_test drives "kitwork/napas" the way a program does: an import, then Invoke by name.
package napas_test

import (
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/kitwork/engine2/core/value"
	"github.com/kitwork/engine2/library/napas"
)

// chain runs napas.bank(…).amount(…)…payload() as the compiled program would: one import, then each
// step an Invoke.
func chain(steps ...[]value.Value) value.Value {
	v := value.Struct(&napas.Napas{}) // kitwork.napas: the application makes one per import
	for _, step := range steps {
		v = v.Invoke(step[0].Text(), step[1:])
	}
	return v
}

func step(name string, arguments ...value.Value) []value.Value {
	return append([]value.Value{value.String(name)}, arguments...)
}

func TestTheProgramSeesTheChainOnly(t *testing.T) {
	want := "amount bank city dynamic info payload receiver static toAccount toCard"
	if got := strings.Join(value.Expose[napas.Napas]().Names(), " "); got != want {
		t.Errorf("napas has %q, want %q", got, want)
	}
}

func TestPayloadIsTheEMVTextWithItsCRC(t *testing.T) {
	qr := chain(
		step("bank", value.String("970415"), value.String("1234567890")),
		step("amount", value.Number(150000)),
		step("receiver", value.String("NGUYEN VAN A")),
		step("info", value.String("Ung ho")),
		step("payload"),
	)
	if qr.IsInvalid() {
		t.Fatal(qr.Failure())
	}
	payload := qr.Text()
	for _, part := range []string{"000201", "010211", "0006970415", "01101234567890", "0208QRIBFTTA", "5303704", "5406150000", "5802VN", "5912NGUYEN VAN A", "62100806Ung ho", "6304"} {
		if !strings.Contains(payload, part) {
			t.Errorf("payload lacks %q: %s", part, payload)
		}
	}
	if strings.Index(payload, "6304") != len(payload)-8 {
		t.Errorf("the CRC tag and four hex digits end the payload: %s", payload)
	}
	if card := chain(step("bank", value.String("970415"), value.String("1")), step("toCard"), step("dynamic"), step("payload")).Text(); !strings.Contains(card, "0208QRIBFTTC") || !strings.Contains(card, "010212") {
		t.Errorf("toCard and dynamic change the service and the method: %s", card)
	}
}

// TestPayloadMatchesTheFirstEngine builds the payloads engine/utilities/napas made, byte for byte: the buffer-built
// text must be exactly what the string-built one was. Each want is the first engine's payload, printed from it on
// 11/11/2026 before engine2 stopped importing it.
func TestPayloadMatchesTheFirstEngine(t *testing.T) {
	for _, c := range []struct{ bin, account, amount, name, city, info, want string }{
		{"970415", "1234567890", "150000", "NGUYEN VAN A", "", "Ung ho",
			"00020101021138540010A00000072701240006970415011012345678900208QRIBFTTA530370454061500005802VN5912NGUYEN VAN A62100806Ung ho630433BD"},
		{"970436", "0123456789", "", "", "Ha Noi", "",
			"00020101021138540010A00000072701240006970436011001234567890208QRIBFTTA53037045802VN6006Ha Noi6304E485"},
		{"970407", "1", "99.5", "A", "B", "C",
			"00020101021138450010A00000072701150006970407010110208QRIBFTTA5303704540499.55802VN5901A6001B62050801C6304F303"},
		{"970422", "123456789012345678901", "1000000000", "TRAN THI B C D E F", "Thanh pho Ho Chi Minh", "Thanh toan don hang 1023 cua khach",
			"00020101021138650010A0000007270135000697042201211234567890123456789010208QRIBFTTA5303704541010000000005802VN5918TRAN THI B C D E F6021Thanh pho Ho Chi Minh62380834Thanh toan don hang 1023 cua khach6304CA79"},
	} {
		steps := []([]value.Value){step("bank", value.String(c.bin), value.String(c.account))}
		if c.amount != "" {
			f, _ := strconv.ParseFloat(c.amount, 64)
			steps = append(steps, step("amount", value.Number(f)))
		}
		if c.name != "" {
			steps = append(steps, step("receiver", value.String(c.name)))
		}
		if c.city != "" {
			steps = append(steps, step("city", value.String(c.city)))
		}
		if c.info != "" {
			steps = append(steps, step("info", value.String(c.info)))
		}
		steps = append(steps, step("payload"))
		if got := chain(steps...).Text(); got != c.want {
			t.Errorf("%+v:\n got %s\nwant %s", c, got, c.want)
		}
	}
}

func TestABankCodeResolvesToItsBIN(t *testing.T) {
	qr := chain(step("bank", value.String("VCB"), value.String("111222333")), step("payload"))
	if !strings.Contains(qr.Text(), "0006970436") {
		t.Errorf("vcb is 970436: %s", qr.Text())
	}
}

func TestAWrongArgumentIsAFailureTheChainCarries(t *testing.T) {
	for name, got := range map[string]value.Value{
		"short BIN":        chain(step("bank", value.String("97"), value.String("1")), step("amount", value.Number(1)), step("payload")),
		"letters in BIN":   chain(step("bank", value.String("97O415"), value.String("1")), step("payload")),
		"no account":       chain(step("bank", value.String("970415")), step("payload")),
		"negative amount":  chain(step("bank", value.String("970415"), value.String("1")), step("amount", value.Number(-5)), step("payload")),
		"amount not a sum": chain(step("bank", value.String("970415"), value.String("1")), step("amount", value.String("abc")), step("payload")),
		"no bank at all":   chain(step("amount", value.Number(1)), step("payload")),
	} {
		if !got.IsInvalid() || got.Code() != "INVALID_ARGUMENT" {
			t.Errorf("%s: want INVALID_ARGUMENT at the end of the chain, got %s", name, got.ToString().Text())
		}
	}
	if got := chain(step("nope")); !got.IsInvalid() || got.Code() != "TYPE" {
		t.Errorf("an unknown step is a TYPE failure: %s", got.ToString().Text())
	}
}

func TestEachImportIsItsOwnChain(t *testing.T) {
	first := value.Struct(&napas.Napas{}).Invoke("bank", []value.Value{value.String("970415"), value.String("1")})
	second := value.Struct(&napas.Napas{}).Invoke("bank", []value.Value{value.String("970436"), value.String("2")})
	if first.Equal(second) {
		t.Error("two imports are two Napas")
	}
	if !strings.Contains(first.Invoke("payload", nil).Text(), "970415") || !strings.Contains(second.Invoke("payload", nil).Text(), "970436") {
		t.Error("each chain keeps its own bank")
	}
	step := first.Invoke("amount", []value.Value{value.Number(5)})
	if step.Equal(first) {
		t.Error("a step returns a new Napas, the one it was called on as it was (docs/napas.md mục 2)")
	}
}

func BenchmarkChain(b *testing.B) {
	bank := step("bank", value.String("970415"), value.String("1234567890"))
	amount := step("amount", value.Number(150000))
	payload := step("payload")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		chain(bank, amount, payload)
	}
}

// A step leaves a frozen Napas as it was and writes into a copy (05/11/2026): a site's file imports napas
// once as the site boots, the host freezes what the file made, and every request of the file chains from
// that import, many at once. Until then a step wrote into the import, so a request's payload carried the
// amount and note of the one before it.
func TestAStepLeavesAFrozenNapasAsItWas(t *testing.T) {
	imported := value.Struct(&napas.Napas{}).Freeze()
	first := imported.Invoke("bank", []value.Value{value.String("970415"), value.String("1234567890")}).
		Invoke("amount", []value.Value{value.Number(150000)}).
		Invoke("info", []value.Value{value.String("ORDER-ALICE")})
	if first.Invoke("payload", nil).IsInvalid() {
		t.Fatal(first.Invoke("payload", nil).Failure())
	}
	if got := imported.Invoke("payload", nil); !got.IsInvalid() || !strings.Contains(got.Message(), "bank() was not called") {
		t.Errorf("the import after a chain from it: %s, want it as it was, with no bank", got.Failure()+got.Text())
	}
	second := imported.Invoke("bank", []value.Value{value.String("970415"), value.String("1")}).Invoke("payload", nil).Text()
	if strings.Contains(second, "ORDER-ALICE") || strings.Contains(second, "150000") {
		t.Errorf("a second chain from the import carries the first's note or amount: %s", second)
	}
	// many chains from one import at once, as the requests of a file run: each its own payload (-race)
	var group sync.WaitGroup
	for g := 0; g < 8; g++ {
		group.Add(1)
		go func(g int) {
			defer group.Done()
			for i := 0; i < 100; i++ {
				account := strconv.Itoa(g*1000 + i)
				payload := imported.Invoke("bank", []value.Value{value.String("970415"), value.String(account)}).
					Invoke("info", []value.Value{value.String("N" + account)}).Invoke("payload", nil).Text()
				if !strings.Contains(payload, "N"+account+"6304") {
					t.Errorf("the chain of account %s gives %s", account, payload)
					return
				}
			}
		}(g)
	}
	group.Wait()
}

// Every step returns a new Napas and leaves the one it was called on as it was (docs/napas.md mục 2), so
// a template branches: until 05/11/2026 a step wrote into a Napas no one had frozen, so two banks set on one
// template were one struct, the second's account in both.
func TestEveryStepLeavesItsNapasAsItWas(t *testing.T) {
	template := value.Struct(&napas.Napas{}).Invoke("receiver", []value.Value{value.String("SHOP")})
	hanoi := template.Invoke("bank", []value.Value{value.String("vcb"), value.String("1111")})
	saigon := template.Invoke("bank", []value.Value{value.String("acb"), value.String("2222")})
	if hanoi.Equal(saigon) || template.Equal(hanoi) {
		t.Fatal("a step gave back the Napas it was called on")
	}
	if got := template.Invoke("payload", nil); !got.IsInvalid() {
		t.Errorf("the template after two banks: %s, want it as it was, with no bank", got.Text())
	}
	first, second := hanoi.Invoke("payload", nil).Text(), saigon.Invoke("payload", nil).Text()
	if !strings.Contains(first, "9704360104"+"1111") || !strings.Contains(second, "9704160104"+"2222") {
		t.Errorf("hanoi %s and saigon %s, want each its own bank and account", first, second)
	}
	amount := hanoi.Invoke("amount", []value.Value{value.Number(5)})
	if strings.Contains(hanoi.Invoke("payload", nil).Text(), "54015") || !strings.Contains(amount.Invoke("payload", nil).Text(), "54015") {
		t.Error("amount wrote into the chain it was called on")
	}
}

// A field of EMV QR text holds 99 bytes at most, its length being two digits: a step whose value passes it, alone or
// with what its field holds besides, is payload()'s failure naming the step (15/11/2026: an info of 100 bytes wrote
// ':' for the first digit of its length, and the QR read as nothing). At the edge the payload is whole: each length
// two digits that say what follows.
func TestAFieldPastItsLengthIsTheFailure(t *testing.T) {
	bank := func(account string) []value.Value { return step("bank", value.String("970415"), value.String(account)) }
	for _, c := range []struct {
		name  string
		steps [][]value.Value
		want  string
	}{
		{"an info of 96 bytes, its tag and length besides", [][]value.Value{bank("1"), step("info", value.String(strings.Repeat("a", 96)))}, "info(…) is too long"},
		{"a receiver of 100 bytes", [][]value.Value{bank("1"), step("receiver", value.String(strings.Repeat("a", 100)))}, "receiver(…) is too long"},
		{"a city of 120 bytes", [][]value.Value{bank("1"), step("city", value.String(strings.Repeat("a", 120)))}, "city(…) is too long"},
		{"an account of 56 digits, the bank's fields besides", [][]value.Value{bank(strings.Repeat("1", 56))}, "bank(…)'s account is too long"},
	} {
		got := chain(append(c.steps, step("payload"))...)
		if !got.IsInvalid() || !strings.Contains(got.Message(), c.want) {
			t.Errorf("%s: %v, want the failure saying %q", c.name, got, c.want)
		}
	}
	edge := chain(bank(strings.Repeat("1", 55)), step("receiver", value.String(strings.Repeat("b", 99))), step("city", value.String(strings.Repeat("c", 99))), step("info", value.String(strings.Repeat("d", 95))), step("payload"))
	if edge.IsInvalid() {
		t.Fatalf("the longest fields: %s", edge.Failure())
	}
	if problem := wellFormed(edge.Text(), map[string]map[string]bool{"38": {"01": true}, "62": nil}); problem != "" {
		t.Errorf("the longest fields: %s\n%s", problem, edge.Text())
	}
}

// wellFormed reads EMV text field by field, the fields of a template inside its own (templates names each template
// with the templates inside it): "" when every length is two digits that say what follows, else what is wrong.
func wellFormed(text string, templates map[string]map[string]bool) string {
	for len(text) > 0 {
		if len(text) < 4 {
			return "a field cut short: " + strconv.Quote(text)
		}
		id, digits := text[:2], text[2:4]
		if digits[0] < '0' || digits[0] > '9' || digits[1] < '0' || digits[1] > '9' {
			return "field " + id + " has the length " + strconv.Quote(digits)
		}
		length, _ := strconv.Atoi(digits)
		if len(text) < 4+length {
			return "field " + id + " says " + digits + " bytes, " + strconv.Itoa(len(text)-4) + " follow"
		}
		if inner, template := templates[id]; template {
			nested := map[string]map[string]bool{}
			for name := range inner {
				nested[name] = nil
			}
			if problem := wellFormed(text[4:4+length], nested); problem != "" {
				return "in " + id + ": " + problem
			}
		}
		text = text[4+length:]
	}
	return ""
}

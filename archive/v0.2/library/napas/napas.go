// Package napas is "kitwork/napas": a VietQR payload by the Napas EMV standard, written as a chain
// that starts with the bank and ends with payload().

//	import * as napas from "kitwork/napas";
//	const qr = napas.bank("vcb", "0123456789").amount(150000).info("Don 1023").payload();

// What the program imports is an empty Napas, value.Struct(&napas.Napas{}). Every step returns a new Napas,
// a copy of the one it was called on with its field set, and leaves that one as it was (docs/napas.md mục
// 2): `const store = napas.receiver(…)` at a file's top level is a template, and store.bank(…) twice gives
// two chains, each paying into its own account. A site's file imports napas once, as the site boots, and
// every request of the file chains from what the file made at once, which no step writes. (Until 05/11/2026
// a step wrote into the Napas it was called on unless the host had frozen it: two chains a file branched
// from one template as it booted were one struct, and the first QR paid into the second's account.) A step
// costs that copy. There is no new() and nothing to register: a chain begins wherever it begins, and
// payload() asks for the bank.
package napas

import (
	"math"
	"strconv"

	"github.com/kitwork/engine2/core/value"
)

// Napas is a payload being built: each step sets a field and returns the Napas for the next step. A
// wrong argument is a failure the chain carries to the end, where safe() reads it.
type Napas struct {
	bin, account, amount, info, name, city string
	service, method, country               string // "" means the default: QRIBFTTA, 11, VN
}

// Freeze is what value.Freeze calls on a struct a site's file made as the site booted, for one that keeps
// state a program can change. A Napas has nothing to mark: no step writes into it (step), so it is read by
// many runs at once as it is. Go's only (a method of no result is no word of the program).
func (n *Napas) Freeze() {}

// step is the Napas a step writes into: a copy of n, which stays as it was.
func (n *Napas) step() *Napas {
	next := *n
	return &next
}

func argument(arguments []value.Value, i int) value.Value {
	if i < len(arguments) {
		return arguments[i]
	}
	return value.Undefined()
}

// Bank sets the bank by code ("vcb") or 6-digit BIN, and the account number; both are required.
func (n *Napas) Bank(arguments ...value.Value) value.Value {
	code, failure := argument(arguments, 0).ToText()
	if failure.IsInvalid() {
		return failure // an object whose own toString fails, or a site's boot made (05/11/2026: read as "")
	}
	bin := resolveBIN(code)
	if len(bin) != 6 {
		return value.Fail(value.InvalidArgumentCode, "bank: BIN must be 6 digits, got "+strconv.Quote(bin))
	}
	for i := 0; i < 6; i++ {
		if bin[i] < '0' || bin[i] > '9' {
			return value.Fail(value.InvalidArgumentCode, "bank: BIN must be digits, got "+strconv.Quote(bin))
		}
	}
	account, failure := argument(arguments, 1).ToText()
	if failure.IsInvalid() {
		return failure
	}
	if account == "" || argument(arguments, 1).IsNullish() {
		return value.Fail(value.InvalidArgumentCode, "bank: account number is required")
	}
	next := n.step()
	next.bin, next.account = bin, account
	return value.Struct(next)
}

// Amount is the transaction amount in VND: a positive number, written without a fraction when whole.
func (n *Napas) Amount(arguments ...value.Value) value.Value {
	amount := argument(arguments, 0).ToNumber()
	if amount.IsInvalid() {
		return amount // as Bank's
	}
	f := amount.Number()
	if f != f || math.IsInf(f, 0) || f < 0 {
		return value.Fail(value.InvalidArgumentCode, "amount: must be a positive number")
	}
	next := n.step()
	next.amount = strconv.FormatFloat(f, 'f', -1, 64)
	return value.Struct(next)
}

// Info is the transfer note (EMV additional data, field 62/08).
func (n *Napas) Info(arguments ...value.Value) value.Value {
	info, failure := argument(arguments, 0).ToText()
	if failure.IsInvalid() {
		return failure // as Bank's
	}
	next := n.step()
	next.info = info
	return value.Struct(next)
}

// Receiver is the merchant or receiver name (field 59).
func (n *Napas) Receiver(arguments ...value.Value) value.Value {
	name, failure := argument(arguments, 0).ToText()
	if failure.IsInvalid() {
		return failure // as Bank's
	}
	next := n.step()
	next.name = name
	return value.Struct(next)
}

// City is the merchant city (field 60).
func (n *Napas) City(arguments ...value.Value) value.Value {
	city, failure := argument(arguments, 0).ToText()
	if failure.IsInvalid() {
		return failure // as Bank's
	}
	next := n.step()
	next.city = city
	return value.Struct(next)
}

// ToCard sends to a card number (service QRIBFTTC); ToAccount, the default, to an account.
func (n *Napas) ToCard(...value.Value) value.Value {
	next := n.step()
	next.service = "QRIBFTTC"
	return value.Struct(next)
}

func (n *Napas) ToAccount(...value.Value) value.Value {
	next := n.step()
	next.service = "QRIBFTTA"
	return value.Struct(next)
}

// Dynamic marks a one-time payload (point of initiation 12); Static, the default, a reusable one (11).
func (n *Napas) Dynamic(...value.Value) value.Value {
	next := n.step()
	next.method = "12"
	return value.Struct(next)
}

func (n *Napas) Static(...value.Value) value.Value {
	next := n.step()
	next.method = "11"
	return value.Struct(next)
}

// Payload ends the chain: the EMV text with its CRC16, as a QR code encodes it. A field holds 99 bytes at most, its
// length being two digits: a step whose value passes it, alone or with what its field holds besides (the account with
// the bank's BIN, the info with its tag), is the failure, naming the step, where a QR no bank's app reads was made.
func (n *Napas) Payload(...value.Value) value.Value {
	if n.bin == "" {
		return value.Fail(value.InvalidArgumentCode, "payload: bank() was not called")
	}
	e := &emv{b: make([]byte, 0, 160)}
	e.field("00", "", "01")               // payload format indicator
	e.field("01", "", or(n.method, "11")) // point of initiation method
	account := e.open("38")               // consumer account information
	e.field("00", "", "A000000727")       // Napas AID
	member := e.open("01")                // the account at the bank
	e.field("00", "bank(…)", n.bin)
	e.field("01", "bank(…)'s account", n.account)
	e.close(member, "bank(…)'s account")
	e.field("02", "", or(n.service, "QRIBFTTA")) // transfer to account or to card
	e.close(account, "bank(…)'s account")
	e.field("53", "", "704") // transaction currency, VND
	if n.amount != "" {
		e.field("54", "amount(…)", n.amount)
	}
	e.field("58", "", or(n.country, "VN"))
	if n.name != "" {
		e.field("59", "receiver(…)", n.name)
	}
	if n.city != "" {
		e.field("60", "city(…)", n.city)
	}
	if n.info != "" {
		additional := e.open("62")
		e.field("08", "info(…)", n.info)
		e.close(additional, "info(…)")
	}
	if e.long != "" {
		return value.Fail(value.InvalidArgumentCode, "payload: "+e.long+" is too long: a field of a VietQR holds 99 bytes, what it holds besides counted")
	}
	b := append(e.b, "6304"...) // the CRC tag, included in what the CRC covers
	return value.String(string(appendCRC(b)))
}

func or(set, otherwise string) string {
	if set == "" {
		return otherwise
	}
	return set
}

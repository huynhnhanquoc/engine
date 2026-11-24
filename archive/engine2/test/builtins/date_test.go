package builtins_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/kitwork/engine2/core/value"
)

// dateTexts are the texts Date.parse reads in TestDateParseWithNode: the ISO form and its edges (the
// corpus's hand2-date group among them), and the legacy forms node also reads (what toString and
// toUTCString write, month names, slashes, AM and PM, zone names, offsets, comments).
var dateTexts = []string{
	// the ISO form
	"2024-01-02", "2024-01-02T03:04:05Z", "2024-01-02T03:04:05.678+07:00", "2024-01", "2024",
	"+002024-01-02", "-000001-01-01", "+000000-01-01", "-000000-01-01", "2024-02-30", "2024-02-31",
	"2023-02-29", "2024-01-02T24:00Z", "2024-01-02T25:00Z", "", "2024-01-02T03:04:05.1Z",
	"2024-01-02T03:04:05.123456Z", "2024-01-02T03:04:05.123456789Z", "2024-01-02T03:04:05.1234567891Z",
	"2024-01-02t03:04z", "2024-01-02T03:04:05-00:30", "2024-13-01", "2024-00-10", "2024-01-32",
	"2024-01-00", "2024-01-02T24:01Z", "2024-01-02T23:60Z", "2024-01-02T23:59:60Z",
	"2024-01-02T03:04:05+24:00", "2024-01-02T03:04:05+23:60", "2024-01-02T3:04Z", "2024-01-02T03Z",
	"2024-01-02Z", "2024-01-02T03:04:05+0700", "2024-01-02T03:04:05+07", "2024-01-02T03:04:05 +07:00",
	"2024-01-02T03:04:05GMT", "2024-01-02+07:00", "2024-01-02T03:04:05ZZ", "2024-01-02T03:04:05Z ",
	"2024-01-02T03:04:5Z", "2024-01-02T03:04:05,123Z", "2024-01-02T", "2024-01-02X", "2024-01Z", "2024Z",
	"2024-01-02T03:04:05.Z", "2024-01-02T03:04", "2024-01-02T03:04:05", "2024-01-02T24:00:00.000",
	"2024-01-02T24:00:00.001", "2024-01-02T03:04:05.123+05:45", "2024-01-02T03:04:05.000+00:00",
	"2024-01-02T00:00:00-00:00", "2000-01-01TZ", "2024-W01", "2024-1-2", "2024-01-2", "24-01-02",
	"-271821-04-20T00:00:00Z", "-271821-04-19T23:59:59.999Z", "+275760-09-13T00:00:00Z",
	"+275760-09-13T00:00:00.001Z", "+275760-09-13", "-271821-04-20", "-271821-04-19",
	"+275760-09-13T06:00:00", "-271821-04-20T08:00:00", "1970", "0000", "9999-12-31T23:59:59.999Z",
	// the legacy reading
	"2024-01-02 03:04", "2024-01-02 03:04:05", " 2024-01-02", "2024-01-02 ", "\t2024-01-02", "2024-01-02\n",
	"2024/01/02", "1/2/2024", "1/2", "Jan 2", "Jan 2, 2024", "January 2, 2024 10:30 PM", "2 January 2024",
	"Tue, 02 Jan 2024 03:04:05 GMT", "Tue Jan 02 2024 10:04:05 GMT+0700 (Indochina Time)", "Tue Jan 02 2024",
	"Thu, 01 Jan 1970 00:00:00 GMT", "12:30", "2024-01-02 12:30 PM", "2024-01-02 12:30 AM",
	"2024-01-02 10:30 pm", "Jan 2 2024 10:30:15 EST", "Jan 2 2024 10:30:15 PDT", "Jan 2 2024 10:30:15 UTC+05:30",
	"Jan 2 2024 10:30:15 GMT-0800", "0", "99", "50", "49", "12/31/99", "12/31/49", "Dec 25, 1995",
	"25 Dec 1995 13:30", "Mon, 25 Dec 1995 13:30:00 +0430", "abc", "Jan", "2024 Jan 2", "2 Jan", "Jan 2024",
	"1e3", "2024.01.02", "(comment) Jan 2 2024", "Jan (comment) 2 2024", "Jan 2 2024 (unclosed",
	"Jan 2 2024 (a (nested) comment) 10:00", "2024-01-02 T03:04", "T03:04", "10:30:15.5", "Jan 2 2024 10:30.5",
	"Jan 2 2024 10::", "Jan 2 2024 10:", "2024-01-02 24:00", "2024-01-02 24:00:01", "Jan 2 2024 13:00 PM",
	"Jan 2 2024 0:00 AM", "Jan 2 2024 12:00 AM", "Jan 2 2024 12:00 PM", "Sat, 01-Jan-2000 08:00:00 GMT",
	"Jan 2 2024 +07:00", "Jan 2 2024 10:00 +7", "Jan 2 2024 10:00 +700", "Jan 2 2024 10:00 +07000",
	"Jan 2 2024 10:00 GMT+7:30", "Jan 2 2024 10:00 UTC-", "mon jan 2 2024", "Jan 2 2024 10:00 ut",
	"Jan 2 2024 10:00 Z", "Jan 2 2024 10:00 XYZ", "2024 2 Jan", "Jan 32 2024", "Feb 30 2024", "13/1/2024",
	"1/13/2024", "Jan 2 2024 10:30:15.123456", "Jan 2 2024 10:30:15.", "Jan 2 2024 10:30:15:16",
	"Jan 2 2024 10 30", "2024-01-02 10:30Z", "2024-01-02 10:30 Z", "2024-01-02 10:30+0100",
	"2024-01-02 10:30 +01:00", "Jan 2 2024 GMT", "GMT Jan 2 2024", "Jan 2 2024 10:00 GMT+07:00 extra",
	"Jan-2-2024", "2-Jan-2024", "Jan 2 2024 10:00:00 a.m.", "Jan 2 2024 10am", "Jan 2 2024 10 pm",
	"2024-01-02T10:00:00+07:00 ", "Jan 2 2024 10:00 -", "Jan 2 2024 10:00 )", "Jan 2 2024 10:00 (",
	"Năm 2024", "Jan 2 2024 ١٠:٠٠", "2024-01-02 10:00 +07:00 GMT", "99999 Jan 2", "Jan 2 99999",
	"Jan 2 1000000000", "4294967296/1/1", "1/1/-5", "-5", "+5", "2024-01-02 -07:00",
}

// TestDateParseWithNode compares Date.parse with node's for every text, both in the fixed zone UTC+7 (node
// told so through TZ: Etc/GMT-7, which has no history of other offsets), so the texts read as local
// time are held to JavaScript too.
func TestDateParseWithNode(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not installed")
	}
	local := time.Local
	time.Local = time.FixedZone("UTC+7", 7*60*60)
	t.Cleanup(func() { time.Local = local })

	encoded, _ := json.Marshal(dateTexts)
	command := exec.Command("node", "-e", `
const texts = JSON.parse(process.argv[1]);
process.stdout.write(JSON.stringify(texts.map((text) => String(Date.parse(text)))));`, string(encoded))
	command.Env = append(os.Environ(), "TZ=Etc/GMT-7")
	raw, err := command.Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	var expected []string
	if err := json.Unmarshal(raw, &expected); err != nil {
		t.Fatalf("node output: %v\n%s", err, raw)
	}
	failed := 0
	for i, text := range dateTexts {
		got := call("Date", "parse", value.String(text)).ToString().Text()
		if got != expected[i] {
			failed++
			t.Errorf("Date.parse(%q): node %s, kitwork %s", text, expected[i], got)
		}
	}
	t.Logf("%d of %d texts agree with node", len(dateTexts)-failed, len(dateTexts))
}

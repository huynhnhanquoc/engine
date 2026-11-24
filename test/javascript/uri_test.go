package javascript_test

import "testing"

// encodeURIComponent, decodeURIComponent, encodeURI, decodeURI đối chiếu với node.
func TestURIFunctionsAreJavaScripts(t *testing.T) {
	compare(t, []string{
		`encodeURIComponent.call(null, "a b&c=d/é😀")`, `encodeURIComponent.call(null, "-_.!~*'()")`,
		`encodeURIComponent.call(null, ";/?:@&=+$,#")`, `encodeURIComponent.call(null, "")`, `encodeURIComponent.call(null, "\uD83D")`,
		`encodeURIComponent.call(null, "\uDE00x")`, `encodeURIComponent.call(null, "a\uD83D")`, `encodeURIComponent.call(null, "😀\uDE00")`,
		`encodeURIComponent.call(null, "\u0000\u007f\u0080\u07ff\u0800\uffff")`, `encodeURIComponent.call(null, 12.5)`,
		`encodeURIComponent.call(null, null)`, `encodeURIComponent.call(null, undefined)`, `encodeURIComponent.call(null)`,
		`encodeURIComponent.call(null, [1, 2])`, `encodeURIComponent.call(null, {toString: boom})`, `encodeURIComponent.call(null, "Phường")`,
		`encodeURIComponent.call(null, "%")`, `encodeURIComponent.call(null, "\"<>\\^{|}[]")`, `encodeURIComponent.call(null, true)`,
		`decodeURIComponent.call(null, "a%20b%26")`, `decodeURIComponent.call(null, "%E1%BA%A1")`, `decodeURIComponent.call(null, "%e1%ba%a1")`,
		`decodeURIComponent.call(null, "%F0%9F%98%80")`, `decodeURIComponent.call(null, "%")`, `decodeURIComponent.call(null, "%2")`,
		`decodeURIComponent.call(null, "%G0")`, `decodeURIComponent.call(null, "%C3")`, `decodeURIComponent.call(null, "%C3%28")`,
		`decodeURIComponent.call(null, "%80")`, `decodeURIComponent.call(null, "%ED%A0%80")`, `decodeURIComponent.call(null, "%C0%AF")`,
		`decodeURIComponent.call(null, "%F4%90%80%80")`, `decodeURIComponent.call(null, "%EF%BF%BD")`, `decodeURIComponent.call(null, "%3B%2F%3F")`,
		`decodeURIComponent.call(null, "100%")`, `decodeURIComponent.call(null, "%25")`, `decodeURIComponent.call(null, "abc")`,
		`decodeURIComponent.call(null, "a+b")`, `decodeURIComponent.call(null, "%00")`, `decodeURIComponent.call(null, 5)`,
		`decodeURIComponent.call(null, "\uD83D%41")`, `decodeURIComponent.call(null, "%F8%80%80%80%80")`, `decodeURIComponent.call(null, "%E0%80%80")`,
		`decodeURIComponent.call(null, "%C2%A0")`, `decodeURIComponent.call(null, "%f0%9f%98")`, `decodeURIComponent.call(null, "%E1%BAx")`,
		`decodeURIComponent.call(null)`, `decodeURIComponent.call(null, {toString: boom})`, `decodeURIComponent.call(null, "%%41")`,
		`decodeURIComponent.call(null, "%F4%8F%BF%BF")`, `decodeURIComponent.call(null, "Phường%20")`,
		`encodeURI.call(null, "http://x.com/a b?q=1&r=é#h")`, `encodeURI.call(null, "[]")`, `encodeURI.call(null, "\uD83D")`,
		`encodeURI.call(null, ";/?:@&=+$,#-_.!~*'()")`, `encodeURI.call(null, "%20")`,
		`decodeURI.call(null, "%3B%2F%3F%3A%40%26%3D%2B%24%2C%23%20")`, `decodeURI.call(null, "%3b%2f")`, `decodeURI.call(null, "%E1%BA%A1%2F")`,
		`decodeURI.call(null, "%")`, `decodeURI.call(null, "%41%25")`,
		`["a b", "é", "&"].map(encodeURIComponent)`, `["a%20b"].map(decodeURIComponent)`,
	})
}

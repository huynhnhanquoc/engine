package hydrate

import "testing"

func TestIndexOpcodeClient(t *testing.T) {
	runNodeDOMScript(t, "index-opcode.test.js", `
var kitwork = window.kitwork;
var ir = kitwork.compile('items[index]');
if (JSON.stringify(ir) !== '["[.]",["$","items"],["$","index"]]') throw new Error('index IR must use [.]');
if (kitwork.run(ir, { items: ['a', 'b'], index: 1 }) !== 'b') throw new Error('indexed read failed');
if (kitwork.run(kitwork.compile('items?.[0]'), { items: null }) !== undefined) throw new Error('null-safe read changed');
var cart = { total: 7, checkout: function () { return this.total; } };
if (kitwork.run(kitwork.compile('cart[method]()'), { cart: cart, method: 'checkout' }) !== 7) throw new Error('computed call lost this');
var matrix = [[1, 2]];
kitwork.run(kitwork.compile('matrix[0].push(3)'), { matrix: matrix });
if (matrix[0].join(',') !== '1,2,3') throw new Error('nested receiver mutation changed');
if (kitwork.run(kitwork.compile('cart[key]'), { cart: cart, key: 'constructor' }) !== undefined) throw new Error('blocked key became reachable');
`)
}

func TestIndexOpcodeServerReceiver(t *testing.T) {
	ir, err := Compile("matrix[0].push(3); matrix[0].join(',')")
	if err != nil {
		t.Fatal(err)
	}
	got, err := Eval(ir, map[string]any{"matrix": []any{[]any{float64(1), float64(2)}}})
	if err != nil || got != "1,2,3" {
		t.Fatalf("indexed receiver: got %v, err %v", got, err)
	}
}
